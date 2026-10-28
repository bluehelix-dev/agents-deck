import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'agents_model.dart';
import 'critter.dart';
import 'theme.dart';

/// A workflow at work: its tile twinkles blue behind the critter. Only while
/// its agents run; a finished one stays on the tile a while, still.
bool agentWorkflowing(Agent a) =>
    a.status == AgentStatus.running &&
    ((a.workflow?.phases.any((p) => p.running > 0) ?? false) ||
        a.subagents.any((s) => s.type == 'workflow'));

/// A dim grid of blue pixels behind a working workflow, and about a sixth of
/// them, at random places, each fading up and down softly on its own clock
/// (2.4 to 4.8 s). [seed] (the agent's id) keeps the pattern the same. The
/// grid is 20 cells across, or cells of [cell] px on a larger surface (the
/// open card).
class WorkflowTwinkle extends StatefulWidget {
  const WorkflowTwinkle({super.key, required this.seed, this.cell});

  final String seed;
  final double? cell;

  @override
  State<WorkflowTwinkle> createState() => _WorkflowTwinkleState();
}

class _WorkflowTwinkleState extends State<WorkflowTwinkle> {
  final _tick = ValueNotifier<int>(0);
  CritterClock? _clock;
  late _TwinklePainter _painter = _TwinklePainter(
    _tick,
    widget.seed,
    widget.cell,
  );

  @override
  void didUpdateWidget(WorkflowTwinkle old) {
    super.didUpdateWidget(old);
    if (old.seed != widget.seed || old.cell != widget.cell) {
      _painter = _TwinklePainter(_tick, widget.seed, widget.cell);
    }
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final c = CritterScope.of(context);
    if (c != _clock) {
      _clock?.removeListener(_onTick);
      _clock = c?..addListener(_onTick);
    }
    _onTick();
  }

  // A frame every 100 ms: the fades step anyway.
  void _onTick() => _tick.value = (_clock?.value ?? 0) ~/ 2;

  @override
  void dispose() {
    _clock?.removeListener(_onTick);
    _tick.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      RepaintBoundary(child: CustomPaint(painter: _painter));
}

class _TwinklePainter extends CustomPainter {
  _TwinklePainter(this.tick, String seed, this.cell) : super(repaint: tick) {
    for (final c in seed.codeUnits) {
      _seed = (_seed * 31 + c) & 0x7fffffff;
    }
  }

  final ValueListenable<int> tick;
  final double? cell;
  var _seed = 0;

  /// The lit pixels for a grid of (cols, rows): (x, y, period, phase).
  var _px = <(int, int, double, double)>[];
  (int, int) _grid = (0, 0);

  static const _share = 70 / 400; // of the cells, as on the tile's 20×20
  static const _peak = 0.32;

  List<(int, int, double, double)> _pixels(int cols, int rows) {
    if (_grid != (cols, rows)) {
      _grid = (cols, rows);
      final rnd = math.Random(_seed);
      final cells = [for (var i = 0; i < cols * rows; i++) i]..shuffle(rnd);
      _px = [
        for (final c in cells.take((cols * rows * _share).round()))
          () {
            final dur = 2.4 + rnd.nextDouble() * 2.4;
            return (c % cols, c ~/ cols, dur, rnd.nextDouble() * dur);
          }(),
      ];
    }
    return _px;
  }

  /// Up in four steps over 40 % of its cycle, lit to 60 %, down in four.
  static double _level(double f) {
    if (f < 0.4) return _peak * (f / 0.4 * 4).floor() / 4;
    if (f < 0.6) return _peak;
    return _peak * (1 - ((f - 0.6) / 0.4 * 4).floor() / 4);
  }

  @override
  void paint(Canvas canvas, Size size) {
    if (size.isEmpty) return;
    final c = cell;
    final cols = c == null ? 20 : math.max(1, (size.width / c).floor());
    final rows = c == null ? 20 : math.max(1, (size.height / c).floor());
    final cw = c ?? size.width / cols, ch = c ?? size.height / rows;
    final side = math.min(cw, ch) * 0.3;
    final dim = Paint()..color = DeckHud.workflow.withValues(alpha: 0.10);
    for (var x = 0; x < cols; x++) {
      for (var y = 0; y < rows; y++) {
        canvas.drawRect(Rect.fromLTWH(x * cw, y * ch, side, side), dim);
      }
    }
    final t = tick.value * 0.1;
    final lit = Paint();
    for (final (x, y, dur, phase) in _pixels(cols, rows)) {
      final o = _level(((t + phase) % dur) / dur);
      if (o <= 0) continue;
      lit.color = DeckHud.workflow.withValues(alpha: o);
      canvas.drawRect(Rect.fromLTWH(x * cw, y * ch, side, side), lit);
    }
  }

  @override
  bool shouldRepaint(_TwinklePainter old) =>
      old._seed != _seed || old.cell != cell;
}

/// Diagonal accent stripes sliding across a hungry tile (a finished agent
/// whose result nobody has looked at yet), driven by the shared critter clock.
class HungryStripes extends StatefulWidget {
  const HungryStripes({super.key});

  @override
  State<HungryStripes> createState() => _HungryStripesState();
}

class _HungryStripesState extends State<HungryStripes> {
  final _step = ValueNotifier<int>(0);
  CritterClock? _clock;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final c = CritterScope.of(context);
    if (c != _clock) {
      _clock?.removeListener(_tick);
      _clock = c?..addListener(_tick);
    }
    _tick();
  }

  // One step every 100 ms: a calm, stepped slide.
  void _tick() => _step.value = ((_clock?.value ?? 0) ~/ 2) % 6;

  @override
  void dispose() {
    _clock?.removeListener(_tick);
    _step.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      RepaintBoundary(child: CustomPaint(painter: _StripesPainter(_step)));
}

class _StripesPainter extends CustomPainter {
  _StripesPainter(this.step) : super(repaint: step);
  final ValueListenable<int> step;

  static const period = 24.0; // stripe + gap, along x

  @override
  void paint(Canvas canvas, Size size) {
    canvas.clipRect(Offset.zero & size);
    final p = Paint()..color = DeckHud.accent.withValues(alpha: 0.16);
    final shift = step.value * period / 6;
    for (var x = -size.height - period + shift; x < size.width; x += period) {
      canvas.drawPath(
        Path()
          ..moveTo(x, size.height)
          ..lineTo(x + period / 2, size.height)
          ..lineTo(x + period / 2 + size.height, 0)
          ..lineTo(x + size.height, 0)
          ..close(),
        p,
      );
    }
  }

  @override
  bool shouldRepaint(_StripesPainter old) => false;
}
