import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  Future<void> pumpPhone(
    WidgetTester tester,
    Size physical, {
    AgentsSource? source,
    Future<String?> Function()? importBundle,
  }) async {
    tester.view.physicalSize = physical;
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      PhoneDeck(
        source: source ?? FakeAgentsSource.demo(),
        importBundle: importBundle ?? () async => null,
        animate: false,
      ),
    );
    await tester.pump();
  }

  testWidgets('upright: the demo deck is 3×6 and fills the phone', (
    tester,
  ) async {
    await pumpPhone(tester, const Size(1080, 2340));
    expect(find.byType(AgentsApp), findsOneWidget);
    expect(find.byKey(const Key('phone-pairing')), findsNothing);
    final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
    expect((grid.columns, grid.rows), (3, 6));
    // The board takes the whole width and (nearly) the whole height.
    final board = tester.getRect(find.byType(AgentsApp));
    expect(board.width, 360);
    expect(board.height, greaterThan(700));
  });

  testWidgets('on its side: the panel\'s 4×4', (tester) async {
    await pumpPhone(tester, const Size(2340, 1080));
    final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
    expect((grid.columns, grid.rows), (4, 4));
    final board = tester.getRect(find.byType(AgentsApp));
    expect(board.height, 360);
    expect(board.width, 360);
  });

  test('columnsFor: 3 for a tall screen, 4 for a wide or square one', () {
    expect(PhoneDeck.columnsFor(const Size(360, 780)), 3);
    expect(PhoneDeck.columnsFor(const Size(780, 360)), 4);
    expect(PhoneDeck.columnsFor(const Size(800, 1000)), 4); // a tablet
    expect(PhoneDeck.boardSize(3), const Size(542, 1076));
    expect(PhoneDeck.boardSize(4), const Size(720, 720));
  });

  testWidgets('no bundle: the pairing card imports one', (tester) async {
    final source = HttpAgentsClient(
      loadConfig: () => (null, null),
      unconfiguredPoll: const Duration(hours: 1),
    );
    var asked = 0;
    await pumpPhone(
      tester,
      const Size(1080, 2340),
      source: source,
      importBundle: () async {
        asked++;
        return 'not a pairing bundle: missing url';
      },
    );
    expect(find.byKey(const Key('phone-pairing')), findsOneWidget);
    await tester.tap(find.byKey(const Key('import-bundle')));
    await tester.pump();
    expect(asked, 1);
    expect(find.byKey(const Key('pairing-problem')), findsOneWidget);
    expect(find.text('not a pairing bundle: missing url'), findsOneWidget);
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
    // A real one, from the package's test PKI.
    String pem(String f) =>
        File('../flutter/agents_ui/test/fixtures/pki/$f').readAsStringSync();
    expect(
      checkBundle(
        jsonEncode({
          'url': 'https://192.0.2.10:7341',
          'name': 'phone',
          'ca': pem('ca.pem'),
          'cert': pem('panel.pem'),
          'key': pem('panel.key'),
        }),
      ),
      isNull,
    );
  });
}
