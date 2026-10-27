// A buzz when an agent needs you.
//
// A status board on the desk is only as good as the moment you look at it.
// So when an agent on the deck turns to waiting (a permission prompt, a
// question) the phone vibrates and posts a notification naming the agent
// and what it asks; the notification goes away by itself once the agent
// is answered or stops waiting. What is already waiting when the app
// connects doesn't buzz: you can see that.
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';

/// What the native side does with an alert.
abstract class AlertSink {
  /// Vibrates and shows a notification for agent [id]; the same [id] again
  /// replaces it.
  Future<void> alert(String id, String title, String body);

  /// Takes agent [id]'s notification down.
  Future<void> clearAlert(String id);
}

/// Watches the deck for agents that start waiting on you.
class WaitingAlerts {
  WaitingAlerts({required this.sink, required this.enabled});

  final AlertSink sink;

  /// Off: nothing buzzes (notifications already up still come down).
  final ValueListenable<bool> enabled;

  ValueListenable<AgentsSnapshot>? _source;
  Set<String> _waiting = {};
  bool _primed = false;

  void watch(ValueListenable<AgentsSnapshot> source) {
    _source?.removeListener(_check);
    _source = source;
    source.addListener(_check);
    _check();
  }

  void dispose() {
    _source?.removeListener(_check);
    _source = null;
  }

  void _check() {
    final state = _source?.value.state;
    if (state == null) return; // not connected: nothing changed that we saw
    final now = <String, Agent>{
      for (final a in state.agents)
        if (a.status == AgentStatus.waiting) a.id: a,
    };
    if (!_primed) {
      // The first look: what waits already is on the board, no buzz.
      _primed = true;
      _waiting = now.keys.toSet();
      return;
    }
    for (final MapEntry(key: id, value: a) in now.entries) {
      if (!_waiting.contains(id) && enabled.value) {
        sink.alert(id, titleOf(a), bodyOf(a));
      }
    }
    for (final id in _waiting) {
      if (!now.containsKey(id)) sink.clearAlert(id);
    }
    _waiting = now.keys.toSet();
  }

  /// The agent as the deck names it.
  static String titleOf(Agent a) => a.aiTitle.isNotEmpty ? a.aiTitle : a.title;

  /// What it asks: the prompt's title and the command or question, short.
  static String bodyOf(Agent a) {
    final p = a.waiting;
    if (p == null) return 'needs you';
    final parts = [p.title, p.detail].where((s) => s.trim().isNotEmpty);
    final s = parts.join(' · ');
    return s.length > 140 ? '${s.substring(0, 139)}…' : s;
  }
}
