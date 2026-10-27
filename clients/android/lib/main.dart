import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// The native side (MainActivity.kt): the app's private folder, the system
/// file picker that reads a pairing bundle, the camera app that photographs
/// a QR code, the pairing itself (Pairing.kt), and the screen's wake lock.
const _files = MethodChannel('agents_deck/files');

const appVersion = '0.1.0';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // The board is the whole window: no bars of its own, the system's symbols
  // in light on its black.
  await SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
  SystemChrome.setSystemUIOverlayStyle(
    const SystemUiOverlayStyle(
      statusBarColor: Color(0x00000000),
      statusBarIconBrightness: Brightness.light,
      systemNavigationBarColor: Color(0x00000000),
      systemNavigationBarIconBrightness: Brightness.light,
      systemNavigationBarContrastEnforced: false,
    ),
  );
  final dir = await _files.invokeMethod<String>('dataDir');
  final phone = Phone(dir: dir!, host: ChannelHost(_files));
  await phone.start();
  final source = HttpAgentsClient(
    loadConfig: phone.loadConfig,
    unconfiguredPoll: const Duration(seconds: 2),
  );
  runApp(PhoneDeck(source: source, phone: phone));
}

/// What the shell asks of the phone itself: the native side in the app, a
/// stand-in in tests.
abstract class PhoneHost {
  /// The system document picker: the file's text, or null when cancelled.
  Future<String?> pickBundle();

  /// The system camera app: the text of the QR code in the photo it took,
  /// or null when it was closed without one. Throws a [PlatformException]
  /// when the photo holds no readable code.
  Future<String?> scanQr();

  /// Pairs with the deck a pairing code names (the QR code's payload, or
  /// what `agentctl pair <name>` prints): makes this phone's key, trades the
  /// code for a certificate, and returns the bundle to keep. Throws a
  /// [PlatformException] with the reason when it can't.
  Future<String?> pair(String payload);

  /// The text on the clipboard, or null.
  Future<String?> clipboardText();

  /// Keeps the screen from dimming while the app is in front.
  Future<void> keepScreenOn(bool on);
}

class ChannelHost implements PhoneHost {
  const ChannelHost(this.channel);

  final MethodChannel channel;

  @override
  Future<String?> pickBundle() => channel.invokeMethod<String>('pickBundle');

  @override
  Future<String?> scanQr() => channel.invokeMethod<String>('scanQr');

  @override
  Future<String?> pair(String payload) =>
      channel.invokeMethod<String>('pair', payload);

  @override
  Future<String?> clipboardText() async =>
      (await Clipboard.getData(Clipboard.kTextPlain))?.text;

  @override
  Future<void> keepScreenOn(bool on) =>
      channel.invokeMethod<void>('keepScreenOn', on);
}

/// This phone's own state, in the app's private folder: the pairing bundle
/// (`agents.json`: from a QR code, a pasted pairing code, or
/// `agentctl pair <name> --bundle FILE`) and the settings (`settings.json`).
class Phone {
  Phone({required this.dir, required this.host});

  final String dir;
  final PhoneHost host;

  String get bundlePath => '$dir/${AgentsConfig.fileName}';
  String get settingsPath => '$dir/settings.json';

  /// Whether the screen stays on while the app is in front. On by default:
  /// a status board that goes dark isn't one.
  final keepScreenOn = ValueNotifier<bool>(true);

  (AgentsConfig?, String?) loadConfig() =>
      AgentsConfig.load(paths: [bundlePath]);

  /// The bundle on this phone, or null (not paired, or unreadable).
  AgentsConfig? get config => loadConfig().$1;

  bool get isPaired => File(bundlePath).existsSync();

  /// Reads the settings and applies them. Once, at start.
  Future<void> start() async {
    try {
      final j = jsonDecode(File(settingsPath).readAsStringSync());
      if (j is Map && j['keepScreenOn'] is bool) {
        keepScreenOn.value = j['keepScreenOn'] as bool;
      }
    } catch (_) {
      // No settings yet, or unreadable: the defaults.
    }
    await host.keepScreenOn(keepScreenOn.value);
  }

  Future<void> setKeepScreenOn(bool on) async {
    keepScreenOn.value = on;
    try {
      File(settingsPath).writeAsStringSync(jsonEncode({'keepScreenOn': on}));
    } on FileSystemException {
      // The setting still applies for this run.
    }
    await host.keepScreenOn(on);
  }

  /// Photographs the QR code `agentctl pair <name> --qr` shows and pairs
  /// with the deck it names. Returns what went wrong, or null (also when
  /// the camera was closed without a photo).
  Future<String?> pairByQr() async {
    String? payload;
    try {
      payload = await host.scanQr();
    } on PlatformException catch (e) {
      return e.message ?? e.code;
    }
    if (payload == null) return null;
    return pairWith(payload);
  }

  /// Pairs from a pairing code on the clipboard: what `agentctl pair <name>`
  /// prints, for a phone or tablet without a camera.
  Future<String?> pairByPaste() async {
    final payload = (await host.clipboardText())?.trim() ?? '';
    if (payload.isEmpty) {
      return 'nothing on the clipboard: copy what `agentctl pair phone` prints';
    }
    return pairWith(payload);
  }

  /// Makes this phone's key, trades the pairing [payload] for a certificate
  /// and keeps the bundle.
  Future<String?> pairWith(String payload) async {
    String? bundle;
    try {
      bundle = await host.pair(payload);
    } on PlatformException catch (e) {
      return e.message ?? e.code;
    }
    if (bundle == null) return 'pairing gave no bundle';
    return checkBundle(bundle) ?? saveBundle(bundle);
  }

  /// Lets the user pick a bundle, checks it and keeps it. Returns what was
  /// wrong with the file, or null (also when the picker was cancelled).
  Future<String?> importBundle() async {
    String? raw;
    try {
      raw = await host.pickBundle();
    } on PlatformException catch (e) {
      return e.message ?? e.code;
    }
    if (raw == null) return null;
    return checkBundle(raw) ?? saveBundle(raw);
  }

  /// Writes the bundle where the client reads it; a problem, or null.
  String? saveBundle(String raw) {
    try {
      File(bundlePath).writeAsStringSync(raw, flush: true);
    } on FileSystemException catch (e) {
      return 'could not save the bundle: ${e.message}';
    }
    return null;
  }

  /// Removes the bundle from this phone. The deck still lists the device
  /// until `agentctl devices revoke <name>`.
  void forgetPairing() {
    final f = File(bundlePath);
    if (f.existsSync()) f.deleteSync();
  }
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

/// The three ways onto a deck, as the cards offer them: each resolves to a
/// problem to show, or null.
typedef PairingWays = ({
  Future<String?> Function() scanQr,
  Future<String?> Function() pasteCode,
  Future<String?> Function() importBundle,
});

/// The deck as the desk panel draws it, laid out in the panel's units and
/// scaled to the screen. The 16 slots of a page go 3×6, 4×4 or 6×3,
/// whichever shape is closest to the screen's, so the board fills a phone
/// or a tablet held either way.
///
/// The shell owns the card stage, so the system's back gesture closes an
/// open card (or the settings) before it leaves the app, and a long press on
/// the board opens the settings.
class PhoneDeck extends StatefulWidget {
  const PhoneDeck({
    super.key,
    required this.source,
    required this.phone,
    this.animate = true,
  });

  final AgentsSource source;
  final Phone phone;

  /// False freezes the critters (tests).
  final bool animate;

  /// The panel's units: 170 px tiles, 8 px between them, 8 px around.
  static const tile = 170.0, gap = 8.0, pad = 8.0;

  /// The shapes a page of 16 slots can take: tall (3×6), the panel's square
  /// (4×4) and wide (6×3), as tiles per row.
  static const shapes = [3, 4, 6];

  /// Tiles per row for a screen of this shape: the shape whose aspect ratio
  /// is closest to the screen's, so the least of it stays empty. A phone
  /// upright gets 3, a tablet on its side 6, anything near square 4.
  static int columnsFor(Size screen) {
    if (screen.isEmpty) return 4;
    final want = screen.width / screen.height;
    double miss(int columns) {
      final b = boardSize(columns);
      final have = b.width / b.height;
      return have / want + want / have; // symmetric: 2 at a match
    }

    return shapes.reduce((a, b) => miss(b) < miss(a) ? b : a);
  }

  /// The board's size in panel units for [columns] tiles per row.
  static Size boardSize(int columns) {
    final rows = (16 + columns - 1) ~/ columns;
    return Size(
      columns * tile + (columns - 1) * gap + 2 * pad,
      rows * tile + (rows - 1) * gap + 2 * pad,
    );
  }

  @override
  State<PhoneDeck> createState() => PhoneDeckState();
}

class PhoneDeckState extends State<PhoneDeck> with WidgetsBindingObserver {
  final _stage = GlobalKey<CardMorphState>();
  bool _settingsOpen = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  /// The system's back gesture: closes the settings, then an open card, and
  /// only then (false) leaves the app.
  @override
  Future<bool> didPopRoute() async {
    if (_settingsOpen) {
      setState(() => _settingsOpen = false);
      return true;
    }
    final stage = _stage.currentState;
    if (stage != null && stage.isOpen) {
      stage.close();
      return true;
    }
    return false;
  }

  void _openSettings() {
    if (!_settingsOpen) setState(() => _settingsOpen = true);
  }

  /// Runs one way of pairing and, when it worked, has the client pick the
  /// new bundle up at once.
  Future<String?> _pair(Future<String?> Function() way) async {
    final problem = await way();
    if (problem == null) widget.source.reconnect();
    return problem;
  }

  PairingWays get _ways => (
    scanQr: () => _pair(widget.phone.pairByQr),
    pasteCode: () => _pair(widget.phone.pairByPaste),
    importBundle: () => _pair(widget.phone.importBundle),
  );

  void _forget() {
    widget.phone.forgetPairing();
    widget.source.reconnect();
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
              final columns = PhoneDeck.columnsFor(box.biggest);
              return Center(
                child: FittedBox(
                  child: SizedBox.fromSize(
                    size: PhoneDeck.boardSize(columns),
                    child: Stack(
                      fit: StackFit.expand,
                      children: [
                        GestureDetector(
                          key: const Key('phone-board'),
                          behavior: HitTestBehavior.translucent,
                          onLongPress: _openSettings,
                          // An Overlay for drag-to-rearrange (no Navigator).
                          child: Overlay(
                            initialEntries: [
                              OverlayEntry(
                                builder: (context) => AgentsApp(
                                  source: widget.source,
                                  animate: widget.animate,
                                  columns: columns,
                                  backButton: true, // no edge swipe on a phone
                                  cardHost: (board) =>
                                      CardMorph(key: _stage, child: board),
                                  openCard:
                                      (
                                        tileContext,
                                        tileKey, {
                                        required id,
                                        required fill,
                                        required builder,
                                      }) => _stage.currentState?.open(
                                        tileKey,
                                        fill: fill,
                                        builder: builder,
                                      ),
                                  pairingCard: (problem) => PairingCard(
                                    problem: problem,
                                    ways: _ways,
                                  ),
                                ),
                              ),
                            ],
                          ),
                        ),
                        if (_settingsOpen)
                          SettingsCard(
                            phone: widget.phone,
                            source: widget.source,
                            ways: _ways,
                            onForget: _forget,
                            onClose: () =>
                                setState(() => _settingsOpen = false),
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

/// A deck-style button: filled with the accent, or outlined.
class DeckButton extends StatelessWidget {
  const DeckButton({
    super.key,
    required this.label,
    required this.onTap,
    this.filled = false,
  });

  final String label;
  final VoidCallback? onTap;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final off = onTap == null;
    final line = off ? DeckHud.dim : DeckHud.accent;
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 26, vertical: 16),
        decoration: BoxDecoration(
          color: filled && !off ? DeckHud.accent : DeckHud.panel,
          border: Border.all(color: line, width: 2),
        ),
        child: Text(
          label,
          style: DeckHud.rm(
            20,
            filled && !off ? DeckHud.bg : line,
            weight: 700,
          ),
        ),
      ),
    );
  }
}

/// A deck-style switch: a square knob that slides to the accent side.
class DeckToggle extends StatelessWidget {
  const DeckToggle({super.key, required this.on, required this.onChanged});

  final bool on;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTap: () => onChanged(!on),
      child: Container(
        width: 60,
        height: 32,
        decoration: BoxDecoration(
          color: on ? DeckHud.accent : DeckHud.panel,
          border: Border.all(
            color: on ? DeckHud.accent : DeckHud.dim,
            width: 2,
          ),
        ),
        child: AnimatedAlign(
          duration: const Duration(milliseconds: 120),
          curve: Curves.easeOut,
          alignment: on ? Alignment.centerRight : Alignment.centerLeft,
          child: Container(
            margin: const EdgeInsets.all(4),
            width: 20,
            height: 20,
            color: on ? DeckHud.bg : DeckHud.dim,
          ),
        ),
      ),
    );
  }
}

/// The card's frame: the panel surface with the deck's padding, scrolling
/// when its content is taller than the board.
class _Card extends StatelessWidget {
  const _Card({super.key, required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(8),
      child: Container(
        color: DeckHud.panel,
        padding: const EdgeInsets.fromLTRB(40, 44, 40, 36),
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: children,
          ),
        ),
      ),
    );
  }
}

Text _title(String s) =>
    Text(s, style: DeckHud.rm(30, DeckHud.ink, weight: 700));

Text _lead(String s) => Text(
  s,
  style: DeckHud.mono(size: 16, color: DeckHud.dim).copyWith(height: 1.4),
);

/// Runs one way of pairing at a time for a card, with the label of the one
/// in flight and the last problem.
mixin _PairingRunner<T extends StatefulWidget> on State<T> {
  /// What is in flight ("scanning"), or null.
  String? busy;
  String? problem;

  Future<void> run(String label, Future<String?> Function() way) async {
    if (busy != null) return;
    setState(() {
      busy = label;
      problem = null;
    });
    final p = await way();
    if (!mounted) return;
    setState(() {
      busy = null;
      problem = p;
    });
  }

  /// A button for one way, greyed while another is in flight.
  Widget wayButton(
    String key,
    String label,
    String doing,
    Future<String?> Function() way, {
    bool filled = false,
  }) => DeckButton(
    key: Key(key),
    label: busy == doing ? '$doing…' : label,
    filled: filled,
    onTap: busy != null ? null : () => run(doing, way),
  );

  Widget problemText(String? shown, Key key) => shown == null
      ? const SizedBox.shrink()
      : Padding(
          padding: const EdgeInsets.only(top: 22),
          child: Text(
            shown,
            key: key,
            style: DeckHud.mono(size: 16, color: DeckHud.accent),
          ),
        );
}

/// Not paired yet: the QR code to scan, and the two other ways in.
class PairingCard extends StatefulWidget {
  const PairingCard({super.key, this.problem, required this.ways});

  /// Why the saved bundle wasn't usable (from the client), if any.
  final String? problem;
  final PairingWays ways;

  @override
  State<PairingCard> createState() => _PairingCardState();
}

class _PairingCardState extends State<PairingCard>
    with _PairingRunner<PairingCard> {
  @override
  Widget build(BuildContext context) {
    TextSpan code(String s) =>
        TextSpan(text: s, style: DeckHud.rm(17, DeckHud.accent));
    Widget step(String n, List<InlineSpan> text) => Padding(
      padding: const EdgeInsets.only(bottom: 16),
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
    return _Card(
      key: const Key('phone-pairing'),
      children: [
        const CritterSprite(
          pose: CritterPose.wait,
          width: 90,
          body: DeckHud.ink,
          cut: DeckHud.panel,
        ),
        const SizedBox(height: 22),
        _title('pair with your computer'),
        const SizedBox(height: 10),
        _lead(
          'this phone makes its own key and trades a one-time code for its '
          'certificate. The code is a QR code on the computer running agentctl.',
        ),
        const SizedBox(height: 26),
        step('1', [
          const TextSpan(text: 'on the computer:\n'),
          code('agentctl pair phone --qr'),
        ]),
        step('2', [
          const TextSpan(text: 'photograph the code within two minutes:'),
        ]),
        Padding(
          padding: const EdgeInsets.only(left: 34),
          child: wayButton(
            'scan-qr',
            'scan QR code',
            'scanning',
            widget.ways.scanQr,
            filled: true,
          ),
        ),
        const SizedBox(height: 26),
        _lead(
          'no camera here? copy what `agentctl pair phone` prints and paste '
          'it, or import a bundle from `agentctl pair phone --bundle '
          'phone.json --host <its address>`.',
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 12,
          runSpacing: 10,
          children: [
            wayButton(
              'paste-code',
              'paste the code',
              'pairing',
              widget.ways.pasteCode,
            ),
            wayButton(
              'import-bundle',
              'import phone.json',
              'choosing',
              widget.ways.importBundle,
            ),
          ],
        ),
        problemText(problem ?? widget.problem, const Key('pairing-problem')),
      ],
    );
  }
}

/// This phone's settings: where it is connected, the screen, the pairing.
/// Opens with a long press on the board; back closes it.
class SettingsCard extends StatefulWidget {
  const SettingsCard({
    super.key,
    required this.phone,
    required this.source,
    required this.ways,
    required this.onForget,
    required this.onClose,
  });

  final Phone phone;
  final AgentsSource source;
  final PairingWays ways;
  final VoidCallback onForget;
  final VoidCallback onClose;

  @override
  State<SettingsCard> createState() => _SettingsCardState();
}

class _SettingsCardState extends State<SettingsCard>
    with _PairingRunner<SettingsCard> {
  bool _armed = false; // "forget pairing" wants a second tap

  void _forget() {
    if (!_armed) {
      setState(() => _armed = true);
      return;
    }
    widget.onForget();
    setState(() => _armed = false);
  }

  @override
  Widget build(BuildContext context) {
    final cfg = widget.phone.config;
    final paired = cfg != null;
    Widget row(String label, Widget value, {Key? key}) => Padding(
      padding: const EdgeInsets.only(bottom: 20),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          SizedBox(
            width: 150,
            child: Text(
              label,
              style: DeckHud.mono(size: 16, color: DeckHud.dim),
            ),
          ),
          Expanded(
            child: KeyedSubtree(key: key, child: value),
          ),
        ],
      ),
    );
    Text value(String s, {Color color = DeckHud.ink}) => Text(
      s,
      style: DeckHud.rm(20, color),
      maxLines: 2,
      overflow: TextOverflow.ellipsis,
    );
    return _Card(
      key: const Key('settings-card'),
      children: [
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(child: _title('this phone')),
            DeckButton(
              key: const Key('settings-done'),
              label: 'done',
              onTap: widget.onClose,
            ),
          ],
        ),
        const SizedBox(height: 10),
        _lead(
          paired
              ? 'paired with a deck. the deck knows this phone by its name; '
                    '`agentctl devices` lists it there.'
              : 'not paired yet: scan the QR code from `agentctl pair phone '
                    '--qr` below, or on the board.',
        ),
        const SizedBox(height: 34),
        ValueListenableBuilder<AgentsSnapshot>(
          valueListenable: widget.source.snapshot,
          builder: (context, snap, _) {
            final deck = snap.state?.server.name;
            final link = switch (snap.link) {
              AgentsLink.connected => 'connected',
              AgentsLink.connecting => 'connecting…',
              AgentsLink.offline => 'offline · ${snap.problem ?? 'retrying'}',
              AgentsLink.unconfigured => 'not paired',
            };
            return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                row(
                  'deck',
                  value(deck == null || deck.isEmpty ? '–' : deck),
                  key: const Key('settings-deck'),
                ),
                row(
                  'address',
                  value(
                    paired
                        ? '${cfg.url.host}:${cfg.url.hasPort ? cfg.url.port : 7341}'
                        : '–',
                  ),
                  key: const Key('settings-address'),
                ),
                row(
                  'this phone',
                  value(paired && cfg.name.isNotEmpty ? cfg.name : '–'),
                  key: const Key('settings-name'),
                ),
                row(
                  'link',
                  value(
                    link,
                    color: snap.link == AgentsLink.offline
                        ? DeckHud.accent
                        : DeckHud.ink,
                  ),
                  key: const Key('settings-link'),
                ),
              ],
            );
          },
        ),
        const SizedBox(height: 6),
        ValueListenableBuilder<bool>(
          valueListenable: widget.phone.keepScreenOn,
          builder: (context, on, _) => Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      'keep the screen on',
                      style: DeckHud.rm(20, DeckHud.ink),
                    ),
                    const SizedBox(height: 6),
                    Text(
                      'while the deck is in front, like the panel',
                      style: DeckHud.mono(size: 15, color: DeckHud.dim),
                    ),
                  ],
                ),
              ),
              DeckToggle(
                key: const Key('keep-screen-on'),
                on: on,
                onChanged: widget.phone.setKeepScreenOn,
              ),
            ],
          ),
        ),
        const SizedBox(height: 40),
        Wrap(
          spacing: 14,
          runSpacing: 14,
          children: [
            wayButton(
              'settings-scan',
              paired ? 'pair again with a QR code' : 'scan a QR code',
              'scanning',
              widget.ways.scanQr,
              filled: !paired,
            ),
            wayButton(
              'settings-paste',
              'paste a pairing code',
              'pairing',
              widget.ways.pasteCode,
            ),
            wayButton(
              'settings-import',
              'import a bundle',
              'choosing',
              widget.ways.importBundle,
            ),
            DeckButton(
              key: const Key('forget-pairing'),
              label: _armed ? 'tap again to forget' : 'forget pairing',
              filled: _armed,
              onTap: paired ? _forget : null,
            ),
          ],
        ),
        problemText(problem, const Key('settings-problem')),
        const SizedBox(height: 40),
        Text(
          'agents deck $appVersion · a long press on the board opens this; '
          'a tap on a free tile adds a session.',
          style: DeckHud.mono(
            size: 14,
            color: DeckHud.dim,
          ).copyWith(height: 1.4),
        ),
      ],
    );
  }
}
