import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/main.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('the demo deck fits a phone', (tester) async {
    tester.view.physicalSize = const Size(1080, 2340);
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      PhoneDeck(
        source: FakeAgentsSource.demo(),
        importBundle: () async => null,
        animate: false,
      ),
    );
    await tester.pump();
    expect(find.byType(AgentsApp), findsOneWidget);
    expect(find.byKey(const Key('phone-pairing')), findsNothing);
  });

  testWidgets('no bundle: the pairing card imports one', (tester) async {
    tester.view.physicalSize = const Size(1080, 2340);
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    final source = HttpAgentsClient(
      loadConfig: () => (null, null),
      unconfiguredPoll: const Duration(hours: 1),
    );
    var asked = 0;
    await tester.pumpWidget(
      PhoneDeck(
        source: source,
        importBundle: () async {
          asked++;
          return 'not a pairing bundle: missing url';
        },
        animate: false,
      ),
    );
    await tester.pump();
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
