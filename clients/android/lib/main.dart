import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// The native side (MainActivity.kt): the app's private folder, and the
/// system file picker that reads a pairing bundle.
const _files = MethodChannel('agents_deck/files');

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final dir = await _files.invokeMethod<String>('dataDir');
  final path = '$dir/${AgentsConfig.fileName}';
  final source = HttpAgentsClient(
    loadConfig: () => AgentsConfig.load(paths: [path]),
    unconfiguredPoll: const Duration(seconds: 2), // picks up an import fast
  );
  runApp(PhoneDeck(source: source, importBundle: () => importBundle(path)));
}

/// Lets the user pick a bundle (`agentctl pair <name> --bundle FILE`), checks
/// it and keeps it at [path], the app's private folder. Returns what was
/// wrong with the file, or null (also when the picker was cancelled).
Future<String?> importBundle(String path) async {
  String? raw;
  try {
    raw = await _files.invokeMethod<String>('pickBundle');
  } on PlatformException catch (e) {
    return e.message ?? e.code;
  }
  if (raw == null) return null;
  final problem = checkBundle(raw);
  if (problem != null) return problem;
  try {
    File(path).writeAsStringSync(raw, flush: true);
  } on FileSystemException catch (e) {
    return 'could not save the bundle: ${e.message}';
  }
  return null;
}

/// What is wrong with [raw] as a pairing bundle, or null when it is one.
String? checkBundle(String raw) {
  try {
    final j = jsonDecode(raw);
    if (j is! Map<String, dynamic>) {
      throw const FormatException('not an object');
    }
    AgentsConfig.fromJson(j);
    return null;
  } on FormatException catch (e) {
    return 'not a pairing bundle: ${e.message}';
  }
}

/// The deck as the desk panel draws it, laid out in the panel's units and
/// scaled to the screen. Held upright, the 16 slots go 3×6 so the board
/// fills the phone; on its side (or on a tablet) it is the panel's 4×4.
class PhoneDeck extends StatelessWidget {
  const PhoneDeck({
    super.key,
    required this.source,
    required this.importBundle,
    this.animate = true,
  });

  final AgentsSource source;

  /// Opens the file picker; resolves to a problem to show, or null.
  final Future<String?> Function() importBundle;

  /// False freezes the critters (tests).
  final bool animate;

  /// The panel's units: 170 px tiles, 8 px between them, 8 px around.
  static const tile = 170.0, gap = 8.0, pad = 8.0;

  /// Tiles per row for a screen of this shape: 3 when it is clearly taller
  /// than wide (a phone held upright), else the panel's 4.
  static int columnsFor(Size screen) =>
      screen.height > screen.width * 1.4 ? 3 : 4;

  /// The board's size in panel units for [columns] tiles per row.
  static Size boardSize(int columns) {
    final rows = (16 + columns - 1) ~/ columns;
    return Size(
      columns * tile + (columns - 1) * gap + 2 * pad,
      rows * tile + (rows - 1) * gap + 2 * pad,
    );
  }

  @override
  Widget build(BuildContext context) {
    return WidgetsApp(
      color: DeckHud.accent,
      debugShowCheckedModeBanner: false,
      builder: (context, _) => ColoredBox(
        color: DeckHud.bg,
        child: SafeArea(
          child: LayoutBuilder(
            builder: (context, box) {
              final columns = columnsFor(box.biggest);
              return Center(
                child: FittedBox(
                  child: SizedBox.fromSize(
                    size: boardSize(columns),
                    // An Overlay for drag-to-rearrange (there's no Navigator).
                    child: Overlay(
                      initialEntries: [
                        OverlayEntry(
                          builder: (context) => AgentsApp(
                            source: source,
                            animate: animate,
                            columns: columns,
                            backButton: true, // no edge swipe on a phone
                            pairingCard: (problem) => PairingCard(
                              problem: problem,
                              importBundle: importBundle,
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }
}

/// No bundle on this phone yet: how to get one, and the button to import it.
class PairingCard extends StatefulWidget {
  const PairingCard({super.key, this.problem, required this.importBundle});

  /// Why the saved bundle wasn't usable (from the client), if any.
  final String? problem;
  final Future<String?> Function() importBundle;

  @override
  State<PairingCard> createState() => _PairingCardState();
}

class _PairingCardState extends State<PairingCard> {
  String? _importProblem;
  bool _picking = false;

  Future<void> _import() async {
    if (_picking) return;
    setState(() => _picking = true);
    final problem = await widget.importBundle();
    if (!mounted) return;
    setState(() {
      _picking = false;
      _importProblem = problem;
    });
  }

  @override
  Widget build(BuildContext context) {
    final problem = _importProblem ?? widget.problem;
    TextSpan code(String s) =>
        TextSpan(text: s, style: DeckHud.rm(17, DeckHud.accent));
    Widget step(String n, List<InlineSpan> text) => Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 34,
            child: Text(n, style: DeckHud.rm(20, DeckHud.accent, weight: 700)),
          ),
          Expanded(
            child: Text.rich(
              TextSpan(children: text),
              style: DeckHud.mono(
                size: 17,
                color: DeckHud.ink,
              ).copyWith(height: 1.4),
            ),
          ),
        ],
      ),
    );
    return Padding(
      padding: const EdgeInsets.all(8),
      child: Container(
        key: const Key('phone-pairing'),
        color: DeckHud.panel,
        padding: const EdgeInsets.fromLTRB(40, 44, 40, 36),
        // Scrolls when a long problem pushes the steps past the card.
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const CritterSprite(
                pose: CritterPose.wait,
                width: 90,
                body: DeckHud.ink,
                cut: DeckHud.panel,
              ),
              const SizedBox(height: 26),
              Text(
                'pair with your computer',
                style: DeckHud.rm(30, DeckHud.ink, weight: 700),
              ),
              const SizedBox(height: 10),
              Text(
                'the deck needs a pairing bundle from the computer running '
                'agentctl: its address, its certificate and a key for this phone.',
                style: DeckHud.mono(
                  size: 16,
                  color: DeckHud.dim,
                ).copyWith(height: 1.4),
              ),
              const SizedBox(height: 30),
              step('1', [
                const TextSpan(
                  text: 'on the computer, with its LAN address:\n',
                ),
                code(
                  'agentctl pair phone --bundle phone.json --host 192.0.2.10',
                ),
              ]),
              step('2', [
                const TextSpan(
                  text:
                      'get phone.json onto this phone (a cable, a drive, '
                      'a message to yourself), then delete it on the computer.',
                ),
              ]),
              step('3', [const TextSpan(text: 'import it here:')]),
              const SizedBox(height: 6),
              GestureDetector(
                key: const Key('import-bundle'),
                behavior: HitTestBehavior.opaque,
                onTap: _import,
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 26,
                    vertical: 16,
                  ),
                  decoration: BoxDecoration(
                    color: _picking ? DeckHud.panel : DeckHud.accent,
                    border: Border.all(color: DeckHud.accent, width: 2),
                  ),
                  child: Text(
                    _picking ? 'choosing…' : 'import phone.json',
                    style: DeckHud.rm(
                      20,
                      _picking ? DeckHud.accent : DeckHud.bg,
                      weight: 700,
                    ),
                  ),
                ),
              ),
              if (problem != null) ...[
                const SizedBox(height: 22),
                Text(
                  problem,
                  key: const Key('pairing-problem'),
                  style: DeckHud.mono(size: 16, color: DeckHud.accent),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
