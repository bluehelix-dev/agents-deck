import 'dart:convert';
import 'dart:io';

import 'package:agents_deck_android/alerts.dart';
import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';

/// A deck whose state the test sets; nothing else is called.
class ScriptedSource implements AgentsSource {
  final _snapshot = ValueNotifier<AgentsSnapshot>(
    const AgentsSnapshot(link: AgentsLink.connecting),
  );

  @override
  ValueListenable<AgentsSnapshot> get snapshot => _snapshot;

  /// The fixture fleet with [statuses] applied by agent id.
  void show(Map<String, String> statuses) {
    final j =
        jsonDecode(
              File(
                '../flutter/agents_ui/test/fixtures/agents_state.json',
              ).readAsStringSync(),
            )
            as Map<String, dynamic>;
    for (final a in (j['agents'] as List).cast<Map<String, dynamic>>()) {
      final s = statuses[a['id']];
      if (s == null) continue;
      a['status'] = s;
      if (s != 'waiting') a['waiting'] = null;
      a['waiting'] ??= {
        'id': 'p-${a['id']}',
        'kind': 'question',
        'title': 'which one?',
        'detail': '',
        'context': '',
        'options': [],
      };
    }
    _snapshot.value = AgentsSnapshot(
      link: AgentsLink.connected,
      state: AgentsState.fromJson(j),
    );
  }

  void drop() {
    _snapshot.value = const AgentsSnapshot(link: AgentsLink.offline);
  }

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('${invocation.memberName}');
}

class RecordingSink implements AlertSink {
  final alerts = <(String, String, String)>[];
  final cleared = <String>[];

  @override
  Future<void> alert(String id, String title, String body) async =>
      alerts.add((id, title, body));

  @override
  Future<void> clearAlert(String id) async => cleared.add(id);
}

void main() {
  late ScriptedSource source;
  late RecordingSink sink;
  late ValueNotifier<bool> enabled;
  late WaitingAlerts alerts;

  setUp(() {
    source = ScriptedSource();
    sink = RecordingSink();
    enabled = ValueNotifier(true);
    alerts = WaitingAlerts(sink: sink, enabled: enabled)
      ..watch(source.snapshot);
  });
  tearDown(() => alerts.dispose());

  test('what waits at the first look is seen, not buzzed', () {
    source.show({'h0m3l4': 'waiting'}); // waiting in the fixture already
    expect(sink.alerts, isEmpty);
  });

  test('an agent turning to waiting buzzes, with the agent and the ask', () {
    source.show({'h0m3l4': 'waiting'});
    source.show({'h0m3l4': 'waiting', 'k3f9a2': 'waiting'});
    expect(sink.alerts, hasLength(1));
    final (id, title, body) = sink.alerts.single;
    expect(id, 'k3f9a2');
    expect(title, isNotEmpty);
    expect(body, 'which one?');
    // The fixture's own prompt: title and command.
    source.show({'h0m3l4': 'idle', 'k3f9a2': 'waiting'});
    source.show({'h0m3l4': 'waiting', 'k3f9a2': 'waiting'});
    expect(sink.alerts.last.$1, 'h0m3l4');
    expect(sink.alerts.last.$2, 'Split the staging network');
    expect(
      sink.alerts.last.$3,
      'run this command? · terraform apply -target=module.staging_net',
    );
  });

  test('answered: the notification comes down; no repeat while it waits', () {
    source.show({'h0m3l4': 'idle'});
    source.show({'h0m3l4': 'waiting'});
    source.show({'h0m3l4': 'waiting'});
    expect(sink.alerts, hasLength(1));
    source.show({'h0m3l4': 'running'});
    expect(sink.cleared, ['h0m3l4']);
  });

  test('off: no buzz, but a notification still comes down', () {
    source.show({'h0m3l4': 'idle'});
    enabled.value = false;
    source.show({'h0m3l4': 'waiting'});
    expect(sink.alerts, isEmpty);
    source.show({'h0m3l4': 'idle'});
    expect(sink.cleared, ['h0m3l4']);
  });

  test('a dropped link changes nothing; a new prompt after it buzzes', () {
    source.show({'h0m3l4': 'idle'});
    source.drop();
    source.show({'h0m3l4': 'waiting'});
    expect(sink.alerts, hasLength(1));
  });

  test('a long ask is cut short', () {
    final a = Agent(
      id: 'x',
      slot: 0,
      title: 't',
      status: AgentStatus.waiting,
      waiting: AgentPrompt(
        id: 'p',
        kind: PromptKind.permission,
        title: 'run this command?',
        detail: 'x' * 200,
        context: '',
        options: const [],
      ),
    );
    expect(WaitingAlerts.bodyOf(a).length, 140);
    expect(WaitingAlerts.bodyOf(a), endsWith('…'));
  });
}
