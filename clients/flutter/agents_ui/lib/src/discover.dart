import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'agents_config.dart';

/// Where the daemon listens for the question (`backend/internal/discover`):
/// a LAN-only multicast group, next to the listener's 7341.
const discoverGroup = '239.255.77.73';
const discoverPort = 7342;
const discoverQuery = 'agents-deck?';

/// Asks the network where the deck is, and returns the listener URLs of the
/// deck this device is paired with: the ones in the answer whose `ca` is the
/// fingerprint of [cfg]'s CA certificate, as the deck ordered them (the
/// address on the asker's network first). Empty when nothing answered
/// within [timeout], when the answers were other decks, or when the
/// network doesn't carry multicast.
///
/// The answer is only a hint where to connect: the connection itself still
/// verifies the server against the CA, as always.
Future<List<Uri>> discoverDeck(
  AgentsConfig cfg, {
  Duration timeout = const Duration(milliseconds: 1500),
  List<InternetAddress>? targets,
  int port = discoverPort,
}) async {
  final want = caFingerprint(cfg.ca);
  if (want == null || cfg.isLocal) return const [];
  final RawDatagramSocket socket;
  try {
    socket = await RawDatagramSocket.bind(InternetAddress.anyIPv4, 0);
  } catch (_) {
    return const [];
  }
  final found = <Uri>[];
  final seen = <String>{};
  final done = Completer<void>();
  final sub = socket.listen((event) {
    if (event != RawSocketEvent.read) return;
    final d = socket.receive();
    if (d == null) return;
    for (final u in parseDiscoverReply(d.data, want)) {
      if (seen.add(u.toString())) found.add(u);
    }
  });
  try {
    socket.broadcastEnabled = true;
    final data = utf8.encode(discoverQuery);
    // The group, and the broadcast address for a network that drops
    // multicast but passes broadcast.
    for (final t
        in targets ??
            [
              InternetAddress(discoverGroup),
              InternetAddress('255.255.255.255'),
            ]) {
      try {
        socket.send(data, t, port);
      } catch (_) {
        // That address isn't reachable from here: the other one may be.
      }
    }
    await done.future.timeout(timeout, onTimeout: () {});
  } catch (_) {
    // No network, or sending refused: nothing found.
  } finally {
    await sub.cancel();
    socket.close();
  }
  List<InternetAddress> mine = const [];
  try {
    mine = [
      for (final i in await NetworkInterface.list(
        type: InternetAddressType.IPv4,
      ))
        ...i.addresses,
    ];
  } catch (_) {
    // Not knowing our own addresses only loses the ordering.
  }
  return preferNear(found, mine);
}

/// [urls] with the ones on this device's own networks first (the same /24 as
/// one of [mine]), then the rest in their order: a computer with many
/// adapters (a VPN, virtual machines) names them all, and the one on our
/// Wi-Fi is the one to try first.
List<Uri> preferNear(List<Uri> urls, List<InternetAddress> mine) {
  final nets = {
    for (final a in mine)
      if (a.type == InternetAddressType.IPv4) _net24(a.address),
  };
  return [
    for (final u in urls)
      if (nets.contains(_net24(u.host))) u,
    for (final u in urls)
      if (!nets.contains(_net24(u.host))) u,
  ];
}

String _net24(String ip) {
  final i = ip.lastIndexOf('.');
  return i < 0 ? ip : ip.substring(0, i);
}

/// The listener URLs in one answer, when it is the deck with the CA
/// [want]; empty for anything else (another deck, garbage).
List<Uri> parseDiscoverReply(List<int> data, List<int> want) {
  final Object? j;
  try {
    j = jsonDecode(utf8.decode(data, allowMalformed: true));
  } catch (_) {
    return const [];
  }
  if (j is! Map<String, dynamic>) return const [];
  final ca = j['ca'] is String ? parseFingerprint(j['ca'] as String) : null;
  if (ca == null || !_sameBytes(ca, want)) return const [];
  final urls = j['urls'];
  if (urls is! List) return const [];
  return [
    for (final u in urls)
      if (u is String)
        if (Uri.tryParse(u) case final uri?
            when uri.scheme == 'https' && uri.host.isNotEmpty && uri.hasPort)
          uri,
  ];
}

bool _sameBytes(List<int> a, List<int> b) {
  if (a.length != b.length) return false;
  var diff = 0;
  for (var i = 0; i < a.length; i++) {
    diff |= a[i] ^ b[i];
  }
  return diff == 0;
}
