import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

/// A PEM-shaped certificate: the fingerprint is over the decoded body, so
/// any bytes do for these tests.
String fakePem(String seed) =>
    '-----BEGIN CERTIFICATE-----\n${base64.encode(utf8.encode(seed * 20))}\n-----END CERTIFICATE-----\n';

void main() {
  final ourCa = fakePem('deck');
  final cfg = AgentsConfig(
    url: Uri.parse(
      'https://198.51.100.7:7341',
    ), // the VPN address it was paired with
    ca: ourCa,
    cert: 'c',
    key: 'k',
  );

  group('caFingerprint', () {
    test('digests the first certificate in a PEM string', () {
      final fp = caFingerprint(ourCa);
      expect(fp, hasLength(32));
      expect(caFingerprint('$ourCa${fakePem('other')}'), fp);
      expect(caFingerprint(fakePem('other')), isNot(fp));
      expect(caFingerprint('no pem here'), isNull);
      expect(
        caFingerprint(
          '-----BEGIN CERTIFICATE-----\n!!\n-----END CERTIFICATE-----',
        ),
        isNull,
      );
    });
  });

  group('parseDiscoverReply', () {
    final want = caFingerprint(ourCa)!;
    String reply(String ca, List<String> urls) => jsonEncode({
      'deck': 'deck-pc',
      'urls': urls,
      'ca': ca,
      'server': 'sha256:00',
      'version': '0.0.0-test',
    });

    test('takes the urls of the deck with our CA, in its order', () {
      final urls = parseDiscoverReply(
        utf8.encode(
          reply(formatFingerprint(want), [
            'https://192.0.2.10:7341',
            'https://198.51.100.7:7341',
          ]),
        ),
        want,
      );
      expect(urls.map((u) => u.toString()), [
        'https://192.0.2.10:7341',
        'https://198.51.100.7:7341',
      ]);
    });

    test('another deck, garbage and odd urls give nothing', () {
      final other = formatFingerprint(caFingerprint(fakePem('other'))!);
      expect(
        parseDiscoverReply(
          utf8.encode(reply(other, ['https://192.0.2.10:7341'])),
          want,
        ),
        isEmpty,
      );
      expect(parseDiscoverReply(utf8.encode('not json'), want), isEmpty);
      expect(parseDiscoverReply(utf8.encode('[1,2]'), want), isEmpty);
      expect(
        parseDiscoverReply(
          utf8.encode(
            reply(formatFingerprint(want), [
              'http://192.0.2.10:7341',
              'https://192.0.2.10',
              'nonsense',
              '://',
            ]),
          ),
          want,
        ),
        isEmpty,
      );
    });
  });

  group('preferNear', () {
    test('our own /24 first, the rest in the deck\'s order', () {
      final urls = [
        'https://172.16.5.1:7341', // a virtual switch on the computer
        'https://198.51.100.7:7341', // its VPN address
        'https://192.0.2.10:7341', // the Wi-Fi we are on
      ].map(Uri.parse).toList();
      final near = preferNear(urls, [InternetAddress('192.0.2.44')]);
      expect(near.map((u) => u.host), [
        '192.0.2.10',
        '172.16.5.1',
        '198.51.100.7',
      ]);
      // Nothing of ours: the order stays.
      expect(preferNear(urls, const []), urls);
    });
  });

  group('discoverDeck', () {
    test('asks on the wire and returns the matching answers', () async {
      final want = caFingerprint(ourCa)!;
      final deck = await RawDatagramSocket.bind(
        InternetAddress.loopbackIPv4,
        0,
      );
      final otherDeck = await RawDatagramSocket.bind(
        InternetAddress.loopbackIPv4,
        0,
      );
      addTearDown(deck.close);
      addTearDown(otherDeck.close);
      final asked = <String>[];
      void answer(RawDatagramSocket s, String ca) {
        s.listen((e) {
          if (e != RawSocketEvent.read) return;
          final d = s.receive();
          if (d == null) return;
          asked.add(utf8.decode(d.data));
          s.send(
            utf8.encode(
              jsonEncode({
                'deck': 'x',
                'urls': ['https://192.0.2.10:7341'],
                'ca': ca,
              }),
            ),
            d.address,
            d.port,
          );
        });
      }

      answer(deck, formatFingerprint(want));
      answer(otherDeck, formatFingerprint(caFingerprint(fakePem('other'))!));
      // Both listen on their own port: the query goes to each.
      final urls = <Uri>[];
      for (final s in [deck, otherDeck]) {
        urls.addAll(
          await discoverDeck(
            cfg,
            targets: [InternetAddress.loopbackIPv4],
            port: s.port,
            timeout: const Duration(milliseconds: 400),
          ),
        );
      }
      expect(asked, [discoverQuery, discoverQuery]);
      expect(urls.map((u) => u.toString()), ['https://192.0.2.10:7341']);
    });

    test('nothing on the network: empty after the timeout', () async {
      final silent = await RawDatagramSocket.bind(
        InternetAddress.loopbackIPv4,
        0,
      );
      addTearDown(silent.close);
      final urls = await discoverDeck(
        cfg,
        targets: [InternetAddress.loopbackIPv4],
        port: silent.port,
        timeout: const Duration(milliseconds: 200),
      );
      expect(urls, isEmpty);
    });

    test('the local socket never asks', () async {
      expect(await discoverDeck(AgentsConfig.unix('/tmp/x.sock')), isEmpty);
    });
  });
}
