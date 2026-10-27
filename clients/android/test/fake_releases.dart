import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/updates.dart';
import 'package:crypto/crypto.dart';

/// GitHub, recorded: a releases listing, a SHA256SUMS and an APK's bytes.
class FakeReleaseSource implements ReleaseSource {
  FakeReleaseSource({String? listing, List<int>? apk, this.sums})
    : listing = listing ?? '[]',
      apk = apk ?? const [];

  String listing;
  List<int> apk;

  /// The SHA256SUMS text; null means the release has none.
  String? sums;

  /// Thrown by every request, when set.
  Object? failWith;

  /// What was downloaded, in order.
  final downloads = <Uri>[];

  @override
  Future<String> text(Uri url) async {
    if (failWith case final e?) throw e;
    if (url.path.endsWith('/releases')) return listing;
    if (url.path.endsWith('SHA256SUMS')) return sums ?? '';
    throw HttpException('not found', uri: url);
  }

  @override
  Future<void> download(
    Uri url,
    File into, {
    void Function(int got, int total)? onProgress,
  }) async {
    if (failWith case final e?) throw e;
    downloads.add(url);
    final sink = into.openWrite();
    final half = apk.length ~/ 2;
    sink.add(apk.sublist(0, half));
    onProgress?.call(half, apk.length);
    sink.add(apk.sublist(half));
    onProgress?.call(apk.length, apk.length);
    await sink.close();
  }
}

/// One release as GitHub lists it, carrying the app (unless [withApk] says
/// otherwise) and SHA256SUMS (unless [withSums] says otherwise); the tag
/// `draft` is a draft.
Map<String, Object?> release(
  String tag, {
  bool withApk = true,
  bool withSums = true,
  int size = 4,
  String? publishedAt,
}) => {
  'tag_name': tag,
  'draft': tag == 'draft',
  'prerelease': tag.contains('rc'),
  'published_at': publishedAt ?? publishedFor(tag),
  'assets': [
    if (withApk)
      {
        'name': apkName(tag),
        'size': size,
        'browser_download_url':
            'https://github.com/x/y/releases/download/$tag/${apkName(tag)}',
      },
    if (withSums)
      {
        'name': 'SHA256SUMS',
        'size': 92,
        'browser_download_url':
            'https://github.com/x/y/releases/download/$tag/SHA256SUMS',
      },
  ],
};

/// A releases listing, newest first, one plain release per tag.
String listing(
  List<String> tags, {
  bool withApk = true,
  bool withSums = true,
}) => jsonEncode([
  for (final tag in tags) release(tag, withApk: withApk, withSums: withSums),
]);

String apkName(String tag) => 'agents-deck-${tag.replaceFirst('v', '')}.apk';

/// A date that grows with the rc number, so "rc.10" is newer than "rc.9"
/// however the list is ordered.
String publishedFor(String tag) {
  final n = int.tryParse(tag.split('.').last) ?? 0;
  return DateTime.utc(2026, 10, 27, 10, n).toIso8601String();
}

String sha256Of(List<int> bytes) => sha256.convert(bytes).toString();

/// A SHA256SUMS naming [bytes] as the APK of [tag].
String sumsFor(String tag, List<int> bytes) =>
    '${sha256Of(bytes)}  ${apkName(tag)}\n';
