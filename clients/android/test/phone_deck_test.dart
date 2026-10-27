import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/main.dart';
import 'package:agents_deck_android/updates.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_releases.dart';

/// The phone's native side, recorded.
class FakeHost implements PhoneHost {
  String? nextPick;

  /// What the scanner reads: a payload, null (closed), or a
  /// [PlatformException] to throw (the scanner couldn't open).
  Object? nextScan;
  String? clipboard;

  /// What pairing answers for a payload: a bundle, or a [PlatformException].
  Object? Function(String payload)? pairing;
  final paired = <String>[];
  final screenOn = <bool>[];

  /// This build, as the native side would report it (the cache folder is
  /// the test's temp dir).
  String cacheDir = '';
  String apkSha256 = '';

  /// The APKs handed to the installer.
  final installed = <String>[];

  /// Alerts raised and taken down, and whether the permission was asked for.
  final alerts = <(String, String, String)>[];
  final cleared = <String>[];
  int alertsEnabled = 0;

  @override
  Future<String?> pickBundle() async => nextPick;

  @override
  Future<String?> scanQr() async {
    if (nextScan case final PlatformException e) throw e;
    return nextScan as String?;
  }

  @override
  Future<String?> pair(String payload) async {
    paired.add(payload);
    final answer = pairing?.call(payload);
    if (answer case final PlatformException e) throw e;
    return answer as String?;
  }

  @override
  Future<String?> clipboardText() async => clipboard;

  @override
  Future<void> keepScreenOn(bool on) async => screenOn.add(on);

  @override
  Future<AppInfo> appInfo() async => AppInfo(
    version: '0.2.0',
    build: 4,
    apkSha256: apkSha256,
    cacheDir: cacheDir,
  );

  @override
  Future<void> installApk(String path) async => installed.add(path);

  @override
  Future<void> alert(String id, String title, String body) async =>
      alerts.add((id, title, body));

  @override
  Future<void> clearAlert(String id) async => cleared.add(id);

  @override
  Future<void> enableAlerts() async => alertsEnabled++;
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

const payload =
    '{"url":"https://192.0.2.10:7341","ca":"sha256:0123456789ABCDEF'
    '0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF","code":"abc"}';

void main() {
  late Directory dir;
  late FakeHost host;
  late FakeReleaseSource releases;
  late Phone phone;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('agents-deck-phone');
    host = FakeHost()..cacheDir = dir.path;
    releases = FakeReleaseSource();
    phone = Phone(dir: dir.path, host: host, releases: releases);
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

  /// A client that reads the phone's bundle and never polls on its own.
  HttpAgentsClient unpairedClient() => HttpAgentsClient(
    loadConfig: phone.loadConfig,
    unconfiguredPoll: const Duration(hours: 1),
  );

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

    testWidgets('buzz when an agent needs you: on by default, persists', (
      tester,
    ) async {
      await phone.start();
      expect(phone.alerts.value, isTrue);
      expect(host.alertsEnabled, 1); // the permission is asked for at start
      File(phone.bundlePath).writeAsStringSync(bundle());
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('alerts')));
      await tester.tap(find.byKey(const Key('alerts')));
      await tester.pump();
      expect(phone.alerts.value, isFalse);
      final saved = jsonDecode(File(phone.settingsPath).readAsStringSync());
      expect(saved, {'keepScreenOn': true, 'alerts': false});
      // Off stays off across a start; turning it on asks for the permission.
      final again = Phone(dir: dir.path, host: host, releases: releases);
      await again.start();
      expect(again.alerts.value, isFalse);
      expect(host.alertsEnabled, 1);
      await again.setAlerts(true);
      expect(host.alertsEnabled, 2);
    });

    testWidgets('the deck buzzes the phone when an agent turns to waiting', (
      tester,
    ) async {
      File(phone.bundlePath).writeAsStringSync(bundle());
      final source = FakeAgentsSource.demo();
      await pumpPhone(tester, const Size(1080, 2340), source: source);
      // The demo deck has agents waiting already: seen, not buzzed.
      expect(host.alerts, isEmpty);
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

    testWidgets('pair again with a QR code from the settings', (tester) async {
      File(phone.bundlePath).writeAsStringSync(bundle());
      host.nextScan = payload;
      host.pairing = (_) => bundle();
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      expect(find.text('pair again with a QR code'), findsOneWidget);
      await tester.tap(find.byKey(const Key('settings-scan')));
      await tester.pump();
      expect(host.paired, [payload]);
      expect(find.byKey(const Key('settings-problem')), findsNothing);
    });
  });

  group('device', () {
    test('a tablet from 600 dp on the short side', () {
      expect(PhoneDeck.deviceFor(const Size(360, 780)), 'phone');
      expect(PhoneDeck.deviceFor(const Size(780, 360)), 'phone');
      expect(PhoneDeck.deviceFor(const Size(600, 960)), 'tablet');
      expect(PhoneDeck.deviceFor(const Size(1280, 800)), 'tablet');
    });

    testWidgets('the cards call a tablet a tablet', (tester) async {
      // 2400×3840 at 3× is 800×1280 dp: a tablet, upright.
      final source = unpairedClient();
      await pumpPhone(tester, const Size(2400, 3840), source: source);
      await tester.pump();
      expect(
        find.textContaining('agentctl pair tablet --qr', findRichText: true),
        findsOneWidget,
      );
      File(phone.bundlePath).writeAsStringSync(bundle());
      await tester.longPressAt(
        tester.getCenter(find.byKey(const Key('phone-board'))),
      );
      await tester.pump();
      expect(find.text('this tablet'), findsWidgets);
      expect(find.text('this phone'), findsNothing);
      source.dispose(); // before the test ends: its poll timer must be gone
      await tester.pump();
    });

    testWidgets('and a phone a phone', (tester) async {
      File(phone.bundlePath).writeAsStringSync(bundle());
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      expect(find.text('this phone'), findsWidgets);
      expect(find.text('this tablet'), findsNothing);
    });
  });

  group('updates', () {
    const installed = [1, 2, 3, 4];
    const newer = [5, 6, 7, 8];

    /// A newer release on GitHub than the APK this phone runs.
    void newerRelease() {
      host.apkSha256 = sha256Of(installed);
      releases
        ..listing = listing(['v0.2.0-rc.5'])
        ..apk = newer
        ..sums = sumsFor('v0.2.0-rc.5', newer);
    }

    testWidgets('start checks quietly; the board hints, the settings update', (
      tester,
    ) async {
      newerRelease();
      File(phone.bundlePath).writeAsStringSync(bundle());
      await phone.start();
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.pump();
      expect(phone.updater.state.value.phase, UpdatePhase.available);
      expect(find.byKey(const Key('update-hint')), findsOneWidget);
      expect(find.textContaining('v0.2.0-rc.5 is out'), findsOneWidget);

      // The hint opens the settings, which show the build and the offer.
      await tester.tap(find.byKey(const Key('update-hint')));
      await tester.pump();
      expect(find.byKey(const Key('settings-card')), findsOneWidget);
      expect(find.byKey(const Key('update-hint')), findsNothing);
      Text value(String key) => tester.widget<Text>(
        find.descendant(of: find.byKey(Key(key)), matching: find.byType(Text)),
      );
      expect(value('settings-app').data, '0.2.0 (4)');
      String? status() =>
          tester.widget<Text>(find.byKey(const Key('update-status'))).data;
      expect(status(), startsWith('v0.2.0-rc.5 is out'));

      // Updating downloads, checks and opens the installer. The download
      // and the checksum are real file I/O, which needs the real event loop.
      await tester.ensureVisible(find.byKey(const Key('update-button')));
      await tester.runAsync(() async {
        await tester.tap(find.byKey(const Key('update-button')));
        while (phone.updater.state.value.phase == UpdatePhase.available ||
            phone.updater.state.value.phase == UpdatePhase.downloading) {
          await Future<void>.delayed(const Duration(milliseconds: 20));
        }
      });
      await tester.pump();
      expect(host.installed, ['${dir.path}/agents-deck-0.2.0-rc.5.apk']);
      expect(File(host.installed.single).readAsBytesSync(), newer);
      expect(status(), startsWith('the installer has'));
    });

    testWidgets('later hides the hint for this run', (tester) async {
      newerRelease();
      File(phone.bundlePath).writeAsStringSync(bundle());
      await phone.start();
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.pump();
      await tester.tap(find.byKey(const Key('update-hint')));
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('update-later')));
      await tester.tap(find.byKey(const Key('update-later')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('settings-done')));
      await tester.pump();
      expect(find.byKey(const Key('update-hint')), findsNothing);
      // Still on offer in the settings.
      expect(phone.updater.state.value.phase, UpdatePhase.available);
    });

    testWidgets('up to date, and a check that fails, in the settings', (
      tester,
    ) async {
      host.apkSha256 = sha256Of(installed);
      releases
        ..listing = listing(['v0.2.0-rc.4'])
        ..sums = sumsFor('v0.2.0-rc.4', installed);
      File(phone.bundlePath).writeAsStringSync(bundle());
      await phone.start();
      await pumpPhone(tester, const Size(1080, 2340));
      await tester.pump();
      expect(find.byKey(const Key('update-hint')), findsNothing);
      await tester.longPressAt(boardBackground(tester));
      await tester.pump();
      String? status() =>
          tester.widget<Text>(find.byKey(const Key('update-status'))).data;
      expect(status(), 'up to date: v0.2.0-rc.4 is the latest release');

      releases.failWith = const SocketException('no route');
      await tester.ensureVisible(find.byKey(const Key('update-button')));
      await tester.tap(find.byKey(const Key('update-button')));
      await tester.pumpAndSettle();
      expect(status(), contains('no connection'));
    });
  });

  group('pairing', () {
    testWidgets('no bundle: the pairing card imports one', (tester) async {
      final source = unpairedClient();
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

    testWidgets('scan QR code: the scan, the pairing, the bundle', (
      tester,
    ) async {
      final source = unpairedClient();
      await pumpPhone(tester, const Size(1080, 2340), source: source);
      // The scanner closed without a code: nothing to say.
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(find.byKey(const Key('pairing-problem')), findsNothing);
      // The scanner couldn't open: the native side's reason.
      host.nextScan = PlatformException(code: 'qr', message: 'no QR code');
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(find.text('no QR code'), findsOneWidget);
      expect(host.paired, isEmpty);
      // The deck refuses: its reason.
      host.nextScan = payload;
      host.pairing = (_) =>
          PlatformException(code: 'pair', message: 'pairing code expired');
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(host.paired, [payload]);
      expect(find.text('pairing code expired'), findsOneWidget);
      expect(phone.isPaired, isFalse);
      // Paired: the bundle is kept and the problem gone.
      host.pairing = (_) => bundle();
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(phone.isPaired, isTrue);
      expect(find.byKey(const Key('pairing-problem')), findsNothing);
      source.dispose();
      await tester.pump();
    });

    testWidgets('paste the code: the clipboard, or nothing on it', (
      tester,
    ) async {
      final source = unpairedClient();
      host.pairing = (_) => bundle();
      await pumpPhone(tester, const Size(1080, 2340), source: source);
      await tester.tap(find.byKey(const Key('paste-code')));
      await tester.pump();
      expect(find.textContaining('nothing on the clipboard'), findsOneWidget);
      host.clipboard = ' $payload\n';
      await tester.tap(find.byKey(const Key('paste-code')));
      await tester.pump();
      expect(host.paired, [payload]);
      expect(phone.isPaired, isTrue);
      source.dispose();
      await tester.pump();
    });

    testWidgets('one way at a time', (tester) async {
      final source = unpairedClient();
      final hold = Completer<String?>();
      final host = _SlowHost(hold.future);
      phone = Phone(dir: dir.path, host: host);
      await pumpPhone(tester, const Size(1080, 2340), source: source);
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(find.text('scanning…'), findsOneWidget);
      await tester.tap(find.byKey(const Key('import-bundle')));
      await tester.tap(find.byKey(const Key('scan-qr')));
      await tester.pump();
      expect(host.scans, 1);
      expect(host.picks, 0);
      hold.complete(null);
      await tester.pump();
      await tester.pump();
      expect(find.text('scan QR code'), findsOneWidget);
      source.dispose();
      await tester.pump();
    });

    test(
      'a bundle that pairing hands back is checked before it is kept',
      () async {
        host.pairing = (_) => 'nonsense';
        expect(
          await phone.pairWith(payload),
          startsWith('not a pairing bundle'),
        );
        expect(phone.isPaired, isFalse);
        host.pairing = (_) => bundle();
        expect(await phone.pairWith(payload), isNull);
        expect(phone.isPaired, isTrue);
      },
    );

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

/// A host whose scanner stays open until [scan] completes.
class _SlowHost extends FakeHost {
  _SlowHost(this.scan);

  final Future<String?> scan;
  int scans = 0;
  int picks = 0;

  @override
  Future<String?> scanQr() {
    scans++;
    return scan;
  }

  @override
  Future<String?> pickBundle() async {
    picks++;
    return null;
  }
}
