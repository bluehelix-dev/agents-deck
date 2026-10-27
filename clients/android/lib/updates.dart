// Updating the app from its GitHub releases.
//
// A release of this repository carries the app as `agents-deck-<version>.apk`
// next to a `SHA256SUMS` (docs/releasing.md). The app asks GitHub for the
// newest published release with such an APK and compares that file's
// checksum with the APK it is running from: equal means up to date, anything
// else is an update on offer. There is no version arithmetic to get wrong,
// and a build nobody released (your own) simply counts as not the latest.
//
// Taking the update downloads the APK into the app's cache, checks it against
// SHA256SUMS, and hands it to Android's package installer, which asks the
// user and installs it over the running app (updates.dart stays out of the
// way: the installer is the system's). An update installs only if it is
// signed with the same key as the app it replaces, so every release is built
// with the one keystore.
import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';

/// The GitHub repository whose releases carry the app.
const updateRepo = 'bluehelix-dev/agents-deck';

/// This build, as the native side reports it: the version from pubspec.yaml,
/// the build number, the SHA-256 of the installed APK (hex, lower case) and
/// the folder a downloaded update goes to.
class AppInfo {
  const AppInfo({
    required this.version,
    required this.build,
    required this.apkSha256,
    required this.cacheDir,
  });

  factory AppInfo.fromMap(Map<Object?, Object?> m) => AppInfo(
    version: m['version'] as String? ?? '',
    build: m['build'] as int? ?? 0,
    apkSha256: (m['apkSha256'] as String? ?? '').toLowerCase(),
    cacheDir: m['cacheDir'] as String? ?? '',
  );

  final String version;
  final int build;
  final String apkSha256;
  final String cacheDir;

  /// "0.2.0 (4)".
  String get label => '$version ($build)';
}

/// A release on GitHub that carries the app.
class AppRelease {
  const AppRelease({
    required this.tag,
    required this.apkName,
    required this.apkUrl,
    required this.apkSize,
    required this.sumsUrl,
  });

  final String tag;
  final String apkName;
  final Uri apkUrl;
  final int apkSize;

  /// The release's SHA256SUMS, or null when it has none.
  final Uri? sumsUrl;
}

/// Where releases and their files come from: GitHub, or a stand-in in tests.
abstract class ReleaseSource {
  /// A small text file (the releases listing, SHA256SUMS).
  Future<String> text(Uri url);

  /// Downloads [url] to [into], reporting bytes got and the total (0 when
  /// unknown).
  Future<void> download(
    Uri url,
    File into, {
    void Function(int got, int total)? onProgress,
  });
}

/// GitHub over HTTPS, with the system's roots.
class GitHubReleases implements ReleaseSource {
  GitHubReleases();

  final _client = HttpClient()..userAgent = 'agents-deck-android';

  Future<HttpClientResponse> _get(Uri url, String accept) async {
    final req = await _client.getUrl(url);
    req.headers.set(HttpHeaders.acceptHeader, accept);
    final res = await req.close();
    if (res.statusCode != 200) {
      await res.drain<void>();
      throw HttpException('GitHub answered ${res.statusCode}', uri: url);
    }
    return res;
  }

  @override
  Future<String> text(Uri url) async {
    final res = await _get(url, 'application/vnd.github+json, text/plain');
    return res.transform(utf8.decoder).join();
  }

  @override
  Future<void> download(
    Uri url,
    File into, {
    void Function(int got, int total)? onProgress,
  }) async {
    final res = await _get(url, 'application/octet-stream');
    final total = res.contentLength;
    final sink = into.openWrite();
    var got = 0;
    try {
      await for (final chunk in res) {
        sink.add(chunk);
        got += chunk.length;
        onProgress?.call(got, total < 0 ? 0 : total);
      }
    } finally {
      await sink.close();
    }
  }
}

/// The releases listing of [repo]: the newest first, pre-releases included
/// (`/releases/latest` would skip those, and every release of this fork is
/// one so far).
Uri releasesUrl(String repo) =>
    Uri.parse('https://api.github.com/repos/$repo/releases?per_page=10');

final _apkName = RegExp(r'^agents-deck-.*\.apk$');

/// The newest published release in a GitHub releases listing that carries
/// the app, or null. Drafts are skipped; the listing comes newest first.
AppRelease? pickRelease(String listing) {
  final j = jsonDecode(listing);
  if (j is! List) return null;
  for (final r in j) {
    if (r is! Map || r['draft'] == true) continue;
    final assets = r['assets'];
    if (assets is! List) continue;
    Map? apk, sums;
    for (final a in assets) {
      if (a is! Map) continue;
      final name = a['name'];
      if (name is! String) continue;
      if (apk == null && _apkName.hasMatch(name)) apk = a;
      if (name == 'SHA256SUMS') sums = a;
    }
    if (apk == null) continue;
    final url = apk['browser_download_url'];
    if (url is! String) continue;
    final sumsUrl = sums?['browser_download_url'];
    return AppRelease(
      tag: r['tag_name'] as String? ?? '',
      apkName: apk['name'] as String,
      apkUrl: Uri.parse(url),
      apkSize: apk['size'] as int? ?? 0,
      sumsUrl: sumsUrl is String ? Uri.parse(sumsUrl) : null,
    );
  }
  return null;
}

/// The checksum of [name] in a SHA256SUMS file (`hex  name` per line; a `*`
/// before the name, as sha256sum writes on Windows, is fine), lower case,
/// or null.
String? sumFor(String sums, String name) {
  for (final line in const LineSplitter().convert(sums)) {
    final f = line.trim().split(RegExp(r'\s+'));
    if (f.length < 2) continue;
    final n = f.sublist(1).join(' ');
    if (n == name || n == '*$name') return f[0].toLowerCase();
  }
  return null;
}

enum UpdatePhase {
  /// Not checked yet.
  unknown,
  checking,

  /// The latest release is what's installed.
  none,

  /// A release that isn't what's installed.
  available,
  downloading,

  /// The system's installer has the APK; it asks the user from here.
  installing,
  failed,
}

/// Where an update stands, for the settings card.
class UpdateState {
  const UpdateState(
    this.phase, {
    this.release,
    this.progress = 0,
    this.problem,
  });

  final UpdatePhase phase;

  /// The release in question, from [UpdatePhase.none] on.
  final AppRelease? release;

  /// 0..1 while downloading.
  final double progress;

  /// What went wrong, when [phase] is [UpdatePhase.failed].
  final String? problem;

  static const unknown = UpdateState(UpdatePhase.unknown);
}

/// Checks for and takes updates. The state is for the settings card and the
/// board's hint; the download and the check run one at a time.
class Updater {
  Updater({required this.source, this.repo = updateRepo});

  final ReleaseSource source;
  final String repo;

  final state = ValueNotifier<UpdateState>(UpdateState.unknown);

  /// True once the user said "later": the board's hint stays away for this
  /// run (the settings still offer the update).
  final snoozed = ValueNotifier<bool>(false);

  bool _busy = false;

  /// Looks the newest release up and compares it with [info].
  Future<void> check(AppInfo info) async {
    if (_busy) return;
    _busy = true;
    state.value = const UpdateState(UpdatePhase.checking);
    try {
      final release = pickRelease(await source.text(releasesUrl(repo)));
      if (release == null) {
        throw const FormatException('no release with the app in it');
      }
      final sha = await _sumOf(release);
      state.value = sha == info.apkSha256
          ? UpdateState(UpdatePhase.none, release: release)
          : UpdateState(UpdatePhase.available, release: release);
    } catch (e) {
      state.value = UpdateState(
        UpdatePhase.failed,
        problem: 'could not check for updates: ${_reason(e)}',
      );
    } finally {
      _busy = false;
    }
  }

  /// Downloads the release on offer (or the one that failed), checks it and
  /// opens the installer with it; [installApk] is the native side.
  Future<void> install(
    AppInfo info,
    Future<void> Function(String path) installApk,
  ) async {
    final release = state.value.release;
    if (_busy || release == null) return;
    _busy = true;
    final file = File('${info.cacheDir}/${release.apkName}');
    try {
      final want = await _sumOf(release);
      if (!await _matches(file, want)) {
        state.value = UpdateState(UpdatePhase.downloading, release: release);
        await source.download(
          release.apkUrl,
          file,
          onProgress: (got, total) {
            final of = total > 0 ? total : release.apkSize;
            state.value = UpdateState(
              UpdatePhase.downloading,
              release: release,
              progress: of > 0 ? (got / of).clamp(0, 1).toDouble() : 0,
            );
          },
        );
        if (!await _matches(file, want)) {
          if (file.existsSync()) file.deleteSync();
          throw const FormatException(
            'the download does not match the release\'s SHA256SUMS',
          );
        }
      }
      state.value = UpdateState(UpdatePhase.installing, release: release);
      await installApk(file.path);
    } catch (e) {
      state.value = UpdateState(
        UpdatePhase.failed,
        release: release,
        problem: 'could not update: ${_reason(e)}',
      );
    } finally {
      _busy = false;
    }
  }

  Future<String> _sumOf(AppRelease release) async {
    final sumsUrl = release.sumsUrl;
    if (sumsUrl == null) {
      throw const FormatException('the release has no SHA256SUMS');
    }
    final sha = sumFor(await source.text(sumsUrl), release.apkName);
    if (sha == null) {
      throw FormatException('${release.apkName} is not in SHA256SUMS');
    }
    return sha;
  }

  static Future<bool> _matches(File f, String sha) async {
    if (!f.existsSync()) return false;
    final digest = await sha256.bind(f.openRead()).first;
    return digest.toString() == sha;
  }

  static String _reason(Object e) => switch (e) {
    FormatException(:final message) => message,
    HttpException(:final message) => message,
    SocketException(:final message) => 'no connection ($message)',
    _ => e.toString(),
  };
}
