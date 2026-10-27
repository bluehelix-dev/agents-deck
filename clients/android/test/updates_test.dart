import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/updates.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_releases.dart';

void main() {
  const installed = [1, 2, 3, 4];
  const newer = [5, 6, 7, 8];
  late Directory dir;
  late AppInfo info;

  setUp(() {
    dir = Directory.systemTemp.createTempSync('agents-deck-updates');
    info = AppInfo(
      version: '0.2.0',
      build: 4,
      apkSha256: sha256Of(installed),
      cacheDir: dir.path,
    );
  });
  tearDown(() => dir.deleteSync(recursive: true));

  group('pickRelease', () {
    test('the newest published release that carries the app', () {
      final r = pickRelease(
        jsonEncode([
          release('draft'),
          release('v0.3.0', withApk: false),
          release('v0.2.0-rc.5'),
          release('v0.2.0-rc.4'),
        ]),
      );
      expect(r, isNotNull);
      expect(r!.tag, 'v0.2.0-rc.5');
      expect(r.apkName, 'agents-deck-0.2.0-rc.5.apk');
      expect(
        r.apkUrl.path,
        endsWith('/v0.2.0-rc.5/agents-deck-0.2.0-rc.5.apk'),
      );
      expect(r.apkSize, 4);
      expect(r.sumsUrl?.path, endsWith('/v0.2.0-rc.5/SHA256SUMS'));
    });

    test('the newest by date, however GitHub orders the list', () {
      // GitHub sorts by tag name as text: rc.10 lands behind rc.2.
      final r = pickRelease(
        jsonEncode([
          release('v0.2.0-rc.9'),
          release('v0.2.0-rc.8'),
          release('v0.2.0-rc.2'),
          release('v0.2.0-rc.10'),
          release('v0.2.0-rc.1'),
        ]),
      );
      expect(r?.tag, 'v0.2.0-rc.10');
      // A newer date wins even over a bigger number.
      final later = pickRelease(
        jsonEncode([
          release('v0.2.0-rc.10'),
          release('v0.2.1', publishedAt: '2026-10-27T09:00:00Z'),
        ]),
      );
      expect(later?.tag, 'v0.2.0-rc.10');
    });

    test('no release, or none with the app: null', () {
      expect(pickRelease('[]'), isNull);
      expect(pickRelease(listing(['v0.3.0'], withApk: false)), isNull);
      expect(pickRelease('{"message": "rate limited"}'), isNull);
    });

    test('a release without SHA256SUMS still names the APK', () {
      final r = pickRelease(listing(['v0.2.0-rc.5'], withSums: false));
      expect(r?.apkName, 'agents-deck-0.2.0-rc.5.apk');
      expect(r?.sumsUrl, isNull);
    });
  });

  test('sumFor reads sha256sum output, with or without the star', () {
    const sums =
        'ABCDEF  agents-deck-0.2.0-rc.5.apk\n'
        '012345 *agents-deck-0.2.0-rc.4.apk\n';
    expect(sumFor(sums, 'agents-deck-0.2.0-rc.5.apk'), 'abcdef');
    expect(sumFor(sums, 'agents-deck-0.2.0-rc.4.apk'), '012345');
    expect(sumFor(sums, 'agents-deck-0.2.0-rc.3.apk'), isNull);
  });

  group('check', () {
    test('the latest release is what is installed: up to date', () async {
      final src = FakeReleaseSource(
        listing: listing(['v0.2.0-rc.4']),
        sums: sumsFor('v0.2.0-rc.4', installed),
      );
      final u = Updater(source: src);
      await u.check(info);
      expect(u.state.value.phase, UpdatePhase.none);
      expect(u.state.value.release?.tag, 'v0.2.0-rc.4');
    });

    test('a different APK on GitHub: an update on offer', () async {
      final src = FakeReleaseSource(
        listing: listing(['v0.2.0-rc.5']),
        sums: sumsFor('v0.2.0-rc.5', newer),
      );
      final u = Updater(source: src);
      await u.check(info);
      expect(u.state.value.phase, UpdatePhase.available);
      expect(u.state.value.release?.tag, 'v0.2.0-rc.5');
    });

    test('no connection, no release, no checksums: failed, with why', () async {
      final src = FakeReleaseSource(listing: listing(['v0.2.0-rc.5']));
      src.failWith = const SocketException('unreachable');
      final u = Updater(source: src);
      await u.check(info);
      expect(u.state.value.phase, UpdatePhase.failed);
      expect(u.state.value.problem, contains('no connection'));

      src.failWith = null;
      src.listing = '[]';
      await u.check(info);
      expect(u.state.value.problem, contains('no release with the app'));

      src.listing = listing(['v0.2.0-rc.5'], withSums: false);
      await u.check(info);
      expect(u.state.value.problem, contains('no SHA256SUMS'));
    });
  });

  group('install', () {
    late FakeReleaseSource src;
    late Updater u;
    final installs = <String>[];

    setUp(() async {
      installs.clear();
      src = FakeReleaseSource(
        listing: listing(['v0.2.0-rc.5']),
        apk: newer,
        sums: sumsFor('v0.2.0-rc.5', newer),
      );
      u = Updater(source: src);
      await u.check(info);
    });

    test('downloads into the cache, checks it, opens the installer', () async {
      final seen = <UpdatePhase>[];
      final progress = <double>[];
      u.state.addListener(() {
        seen.add(u.state.value.phase);
        progress.add(u.state.value.progress);
      });
      await u.install(info, (path) async => installs.add(path));
      expect(u.state.value.phase, UpdatePhase.installing);
      expect(installs, ['${dir.path}/agents-deck-0.2.0-rc.5.apk']);
      expect(File(installs.single).readAsBytesSync(), newer);
      expect(seen, contains(UpdatePhase.downloading));
      expect(progress, contains(1.0));
      expect(src.downloads, hasLength(1));
    });

    test('a download that does not match SHA256SUMS is thrown away', () async {
      src.apk = [9, 9, 9, 9];
      await u.install(info, (path) async => installs.add(path));
      expect(u.state.value.phase, UpdatePhase.failed);
      expect(u.state.value.problem, contains('does not match'));
      expect(installs, isEmpty);
      expect(
        File('${dir.path}/agents-deck-0.2.0-rc.5.apk').existsSync(),
        false,
      );
      // The release stays known: the card offers to try again.
      expect(u.state.value.release?.tag, 'v0.2.0-rc.5');
    });

    test('an APK already in the cache is not downloaded again', () async {
      File('${dir.path}/agents-deck-0.2.0-rc.5.apk').writeAsBytesSync(newer);
      await u.install(info, (path) async => installs.add(path));
      expect(u.state.value.phase, UpdatePhase.installing);
      expect(src.downloads, isEmpty);
      expect(installs, hasLength(1));
    });

    test('the installer refusing to open is reported', () async {
      await u.install(
        info,
        (path) async => throw const FormatException('nope'),
      );
      expect(u.state.value.phase, UpdatePhase.failed);
      expect(u.state.value.problem, 'could not update: nope');
    });
  });
}
