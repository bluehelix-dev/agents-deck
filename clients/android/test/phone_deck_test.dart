import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

/// The phone's native side, recorded.
class FakeHost implements PhoneHost {
  String? nextPick;
  final screenOn = <bool>[];

  @override
  Future<String?> pickBundle() async => nextPick;

  @override
  Future<void> keepScreenOn(bool on) async => screenOn.add(on);
}

/// A bundle built from the package's test PKI.
String bundle() {
  String pem(String f) =>
      File('../flutter/agents_ui/test/fixtures/pki/$f').readAsStringSync();
  return jsonEncode({
    'url': 'https://192.0.2.10:7341',
    'name': 'phone',
    'ca': pem('ca.pem'),
    'cert': pem('panel.pem'),
    'key': pem('panel.key'),
  });
}

void main() {
  late Directory dir;
  late FakeHost host;
  late Phone phone;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('agents-deck-phone');
    host = FakeHost();
    phone = Phone(dir: dir.path, host: host);
  });
  tearDown(() => dir.deleteSync(recursive: true));

  Future<void> pumpPhone(
    WidgetTester tester,
    Size physical, {
    AgentsSource? source,
  }) async {
    tester.view.physicalSize = physical;
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      PhoneDeck(
        source: source ?? FakeAgentsSource.demo(),
        phone: phone,
        animate: false,
      ),
    );
    await tester.pump();
  }

  PhoneDeckState shell(WidgetTester tester) =>
      tester.state<PhoneDeckState>(find.byType(PhoneDeck));

  /// The last cell of a 3×6 board is empty background (16 slots, 18 cells).
  Offset boardBackground(WidgetTester tester) {
    final board = tester.getRect(find.byType(AgentsApp));
    final scale = board.width / PhoneDeck.boardSize(3).width;
    const cell = PhoneDeck.tile + PhoneDeck.gap;
    return board.topLeft +
        Offset(
          (PhoneDeck.pad + 2 * cell + PhoneDeck.tile / 2) * scale,
          (PhoneDeck.pad + 5 * cell + PhoneDeck.tile / 2) * scale,
        );
  }

  group('shape', () {
    testWidgets('upright: the demo deck is 3×6 and fills the phone', (
      tester,
    ) async {
      await pumpPhone(tester, const Size(1080, 2340));
      final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
      expect((grid.columns, grid.rows), (3, 6));
      final board = tester.getRect(find.byType(AgentsApp));
      expect(board.width, 360);
      expect(board.height, greaterThan(700));
      expect(find.byKey(const Key('phone-pairing')), findsNothing);
    });

    testWidgets('on its side: 6×3 fills the height', (tester) async {
      await pumpPhone(tester, const Size(2340, 1080));
      final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
      expect((grid.columns, grid.rows), (6, 3));
      expect(tester.getRect(find.byType(AgentsApp)).height, 360);
    });

    testWidgets('a 4:3 tablet gets the panel\'s 4×4', (tester) async {
      await pumpPhone(tester, const Size(2048, 1536));
      final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
      expect((grid.columns, grid.rows), (4, 4));
    });

    test('columnsFor picks the shape closest to the screen', () {
      expect(PhoneDeck.columnsFor(const Size(360, 780)), 3);
      expect(PhoneDeck.columnsFor(const Size(780, 360)), 6);
      expect(PhoneDeck.columnsFor(const Size(800, 1280)), 3);
      expect(PhoneDeck.columnsFor(const Size(1280, 800)), 6);
      expect(PhoneDeck.columnsFor(const Size(768, 1024)), 4);
      expect(PhoneDeck.columnsFor(const Size(1024, 768)), 4);
      expect(PhoneDeck.columnsFor(const Size(720, 720)), 4);
      expect(PhoneDeck.columnsFor(Size.zero), 4);
      expect(PhoneDeck.boardSize(3), const Size(542, 1076));
      expect(PhoneDeck.boardSize(4), const Size(720, 720));
      expect(PhoneDeck.boardSize(6), const Size(1076, 542));
    });
  });

  group('back', () {
    testWidgets('closes an open card before it leaves the app', (tester) async {
      await pumpPhone(tester, const Size(1080, 2340));
      expect(await shell(tester).didPopRoute(), isFalse); // nothing open
      await tester.tap(find.byKey(const ValueKey('agent-tile-k3f9a2')));
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.byKey(const Key('agent-focus')), findsOneWidget);
      expect(await shell(tester).didPopRoute(), isTrue);
      await tester.pump(const Duration(milliseconds: 300));
      expect(find.byKey(const Key('agent-focus')), findsNothing);
      expect(await shell(tester).didPopRoute(), isFalse);
    });

    testWidgets('closes the settings first', (tester) async {
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      expect(find.byKey(const Key('settings-card')), findsOneWidget);
      expect(await shell(tester).didPopRoute(), isTrue);
      await tester.pump();
      expect(find.byKey(const Key('settings-card')), findsNothing);
    });
  });

  group('settings', () {
    testWidgets('show the deck, the address, the name and the link', (
      tester,
    ) async {
      File(phone.bundlePath).writeAsStringSync(bundle());
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      Text value(String key) => tester.widget<Text>(
        find.descendant(of: find.byKey(Key(key)), matching: find.byType(Text)),
      );
      expect(value('settings-deck').data, 'workstation');
      expect(value('settings-address').data, '192.0.2.10:7341');
      expect(value('settings-name').data, 'phone');
      expect(value('settings-link').data, 'connected');
      await tester.tap(find.byKey(const Key('settings-done')));
      await tester.pump();
      expect(find.byKey(const Key('settings-card')), findsNothing);
    });

    testWidgets('keep the screen on: on by default, the toggle persists', (
      tester,
    ) async {
      await phone.start();
      expect(host.screenOn, [true]);
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      await tester.tap(find.byKey(const Key('keep-screen-on')));
      await tester.pump();
      expect(host.screenOn, [true, false]);
      final again = Phone(dir: dir.path, host: FakeHost());
      await again.start();
      expect(again.keepScreenOn.value, isFalse);
    });

    testWidgets('forget pairing takes two taps and removes the bundle', (
      tester,
    ) async {
      File(phone.bundlePath).writeAsStringSync(bundle());
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      await tester.tap(find.byKey(const Key('forget-pairing')));
      await tester.pump();
      expect(find.text('tap again to forget'), findsOneWidget);
      expect(phone.isPaired, isTrue);
      await tester.tap(find.byKey(const Key('forget-pairing')));
      await tester.pump();
      expect(phone.isPaired, isFalse);
    });
  });

  group('pairing', () {
    testWidgets('no bundle: the pairing card imports one', (tester) async {
      final source = HttpAgentsClient(
        loadConfig: phone.loadConfig,
        unconfiguredPoll: const Duration(hours: 1),
      );
      host.nextPick = 'nonsense';
      await pumpPhone(tester, const Size(1080, 2340), source: source);
      expect(find.byKey(const Key('phone-pairing')), findsOneWidget);
      await tester.tap(find.byKey(const Key('import-bundle')));
      await tester.pump();
      expect(find.byKey(const Key('pairing-problem')), findsOneWidget);
      expect(phone.isPaired, isFalse);
      host.nextPick = bundle();
      await tester.tap(find.byKey(const Key('import-bundle')));
      await tester.pump();
      expect(phone.isPaired, isTrue);
      source.dispose(); // before the test ends: its poll timer must be gone
      await tester.pump();
    });

    test('checkBundle tells a bundle from anything else', () {
      expect(checkBundle('nonsense'), startsWith('not a pairing bundle'));
      expect(checkBundle('[1, 2]'), 'not a pairing bundle: not an object');
      expect(
        checkBundle('{"url": "ftp://x"}'),
        startsWith('not a pairing bundle'),
      );
      expect(checkBundle(bundle()), isNull);
    });
  });
}
