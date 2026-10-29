import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'theme.dart';
import 'widgets.dart';
import 'agent_detail.dart';
import 'agents_client.dart';
import 'agents_model.dart';
import 'agents_style.dart';
import 'critter.dart';
import 'live_picker.dart';
import 'twinkle.dart';

/// Agents app: a Stream-Deck-style board of the Mac's coding agents
/// (`agentctl serve`). Chrome-less 4×4 grid, one tile per agent by slot,
/// each tile morphing into that agent's card. The host decides how cards
/// open ([openCard] + [cardHost]); by default the built-in [CardMorph].
///
/// Tiles sit at their slot, so the board can have gaps: arrange it freely.
/// While every agent is in slots 0..15 it's one page; beyond that pages hold
/// 15 slots each plus a pager tile in the last cell (tap = next page).
/// Tile-based paging, because edge swipes may belong to the host (the desk
/// panel's shell uses left = back, top = settings).
class AgentsApp extends StatefulWidget {
  const AgentsApp({
    super.key,
    required this.source,
    this.animate = true,
    this.openCard,
    this.cardHost,
    this.pairingCard,
    this.visible,
    this.backButton = false,
    this.columns = 4,
    this.toolSquare = false,
  }) : assert(
         (openCard == null) == (cardHost == null),
         'openCard and cardHost come together',
       );

  final AgentsSource source;

  /// Opens an agent's card from its tile; null = [CardMorph.open].
  final AgentCardOpener? openCard;

  /// Wraps the board so [openCard] can find its host; null = [CardMorph].
  final Widget Function(Widget board)? cardHost;

  /// A back button on agent cards, for hosts without an edge swipe.
  final bool backButton;

  /// Whether anyone can see the board (a menu bar window that hides): the
  /// critters stop animating while false. Null = always visible.
  final ValueListenable<bool>? visible;

  /// Shown while there are no pairing values (with why saved ones failed);
  /// null = [AgentsPairingCard], the desk panel's setup steps.
  final Widget Function(String? problem)? pairingCard;

  /// False freezes every critter on its first frame (goldens, tests).
  final bool animate;

  /// Tiles per row. 4 is the panel's 4×4; 3 lays the same 16 slots out as
  /// 3×6, which fills a phone held upright.
  final int columns;

  /// A small square in the tool's colour on each tile and card (Claude,
  /// Codex, …). Off by default; an agent in a named Claude account shows
  /// its account's colour either way.
  final bool toolSquare;

  @override
  State<AgentsApp> createState() => _AgentsAppState();
}

class _AgentsAppState extends State<AgentsApp> {
  late final _gridClock = CritterClock(animate: widget.animate);
  late final _cardClock = CritterClock(animate: widget.animate)
    ..running = false;
  int _page = 0;
  final _pager = PageController();

  static const _cells = 16;

  @override
  void dispose() {
    widget.visible?.removeListener(_syncClocks);
    _pager.dispose();
    _gridClock.dispose();
    _cardClock.dispose();
    super.dispose();
  }

  bool _cardShown = false;

  @override
  void initState() {
    super.initState();
    widget.visible?.addListener(_syncClocks);
    _syncClocks();
  }

  /// The card covers the grid: only one set of critters runs at a time, and
  /// none while the board is hidden.
  void _cardVisible(bool shown) {
    _cardShown = shown;
    _syncClocks();
  }

  void _syncClocks() {
    final seen = widget.visible?.value ?? true;
    _gridClock.running = seen && !_cardShown;
    _cardClock.running = seen && _cardShown;
  }

  void _open(BuildContext tileContext, GlobalKey tileKey, Agent a) {
    final p = a.status == AgentStatus.waiting ? a.waiting : null;
    (widget.openCard ?? CardMorph.open)(
      tileContext,
      tileKey,
      id: 'agent/${a.id}',
      // The flight's colour = the card it lands as.
      fill: p?.kind == PromptKind.permission ? DeckHud.accent : DeckHud.bg,
      builder: (close) => CritterScope(
        clock: _cardClock,
        child: AgentDetail(
          source: widget.source,
          agentId: a.id,
          onClose: close,
          onVisible: _cardVisible,
          showBack: widget.backButton,
          toolSquare: widget.toolSquare,
        ),
      ),
    );
  }

  /// A free cell opens "add a session" (hand-started Claude sessions); the
  /// one you pick lands in that cell.
  void _openPicker(BuildContext cellContext, GlobalKey cellKey, int slot) {
    (widget.openCard ?? CardMorph.open)(
      cellContext,
      cellKey,
      id: 'add/$slot',
      fill: DeckHud.bg,
      builder: (close) => LivePicker(
        source: widget.source,
        slot: slot,
        onClose: close,
        showBack: widget.backButton,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return CritterScope(
      clock: _gridClock,
      child: (widget.cardHost ?? (board) => CardMorph(child: board))(
        ValueListenableBuilder<AgentsSnapshot>(
          valueListenable: widget.source.snapshot,
          builder: (context, snap, _) => _body(snap),
        ),
      ),
    );
  }

  Widget _body(AgentsSnapshot snap) {
    final state = snap.state;
    if (snap.link == AgentsLink.unconfigured) {
      return widget.pairingCard?.call(snap.problem) ??
          AgentsPairingCard(problem: snap.problem);
    }
    if (state == null) {
      return _Notice(
        pose: CritterPose.starting,
        title: snap.link == AgentsLink.offline
            ? 'offline · retrying'
            : 'connecting',
        lines: [if (snap.problem != null) snap.problem!],
      );
    }
    final offline = snap.link != AgentsLink.connected;
    final Widget content;
    if (state.agents.isEmpty) {
      content = const _Notice(
        pose: CritterPose.idle,
        title: 'no agents running',
        lines: ['start one with', 'agentctl new'],
        codeLine: 1,
      );
    } else {
      content = _deck(state);
    }
    if (!offline) return content;
    return Stack(
      fit: StackFit.expand,
      children: [
        Opacity(opacity: 0.35, child: content),
        Positioned(
          left: 0,
          right: 0,
          bottom: 22,
          child: Center(child: _OfflineChip(problem: snap.problem)),
        ),
      ],
    );
  }

  /// Long-press a tile and drop it on another to swap the two agents, or on
  /// a free cell to move it there. Positions are stored by the daemon, so the
  /// web board follows.
  Widget _swapCell(
    AgentsState state, {
    required int slot,
    String? id,
    String title = '',
    required Widget child,
  }) {
    return LayoutBuilder(
      builder: (context, box) {
        final target = DragTarget<String>(
          onWillAcceptWithDetails: (d) => d.data != id,
          onAcceptWithDetails: (d) => widget.source.move(d.data, slot),
          builder: (context, candidates, _) => Stack(
            fit: StackFit.expand,
            children: [
              child,
              if (candidates.isNotEmpty)
                IgnorePointer(
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      border: Border.all(color: DeckHud.ink, width: 3),
                    ),
                  ),
                ),
            ],
          ),
        );
        if (id == null) return target;
        return LongPressDraggable<String>(
          data: id,
          delay: const Duration(milliseconds: 350),
          hapticFeedbackOnStart: false,
          feedback: SizedBox(
            width: box.maxWidth,
            height: box.maxHeight,
            child: _DragGhost(title: title),
          ),
          childWhenDragging: Opacity(opacity: 0.35, child: child),
          child: target,
        );
      },
    );
  }

  /// Pages of 16 slots (page = slot ~/ 16), side by side: swipe between
  /// them, use the arrow keys or click a bar. With one page the tiles are
  /// square; with more, they keep their width and get a little shorter so
  /// the bar row fits in the same square.
  Widget _deck(AgentsState state) {
    final last = state.agents.map((a) => a.slot).fold(0, math.max);
    final pages = last ~/ _cells + 1;
    if (_page >= pages) _page = pages - 1;
    final hot = {
      for (final a in state.agents)
        if (a.needsYou) a.slot ~/ _cells,
    };
    return Focus(
      autofocus: true,
      onKeyEvent: (_, e) {
        if (e is! KeyDownEvent) return KeyEventResult.ignored;
        final step = switch (e.logicalKey) {
          LogicalKeyboardKey.arrowLeft => -1,
          LogicalKeyboardKey.arrowRight => 1,
          _ => 0,
        };
        if (step == 0) return KeyEventResult.ignored;
        _goTo((_page + step).clamp(0, pages - 1));
        return KeyEventResult.handled;
      },
      child: Padding(
        padding: EdgeInsets.fromLTRB(8, 8, 8, pages < 2 ? 8 : 0),
        child: Column(
          children: [
            Expanded(
              child: PageView.builder(
                key: const Key('agents-pages'),
                controller: _pager,
                itemCount: pages,
                onPageChanged: (p) => setState(() => _page = p),
                itemBuilder: (context, p) => DeckGrid(
                  cells: _cellsFor(state, p),
                  columns: widget.columns,
                  rows: (_cells + widget.columns - 1) ~/ widget.columns,
                  square: pages < 2,
                ),
              ),
            ),
            if (pages > 1)
              SizedBox(
                height: _barsHeight,
                child: _PageBars(
                  pages: pages,
                  current: _page,
                  hot: hot,
                  onTap: _goTo,
                ),
              ),
          ],
        ),
      ),
    );
  }

  static const _barsHeight = 34.0;

  void _goTo(int page) {
    if (!_pager.hasClients || page == _page) return;
    _pager.animateToPage(
      page,
      duration: const Duration(milliseconds: 260),
      curve: Curves.easeOutCubic,
    );
  }

  List<Widget> _cellsFor(AgentsState state, int page) {
    final bySlot = {for (final a in state.agents) a.slot: a};
    final start = page * _cells;
    return [
      for (var slot = start; slot < start + _cells; slot++)
        if (bySlot[slot] case final a?)
          _swapCell(
            state,
            slot: slot,
            id: a.id,
            title: a.title,
            child: AgentTile(
              key: ValueKey('agent-tile-${a.id}'),
              agent: a,
              state: state,
              onOpen: _open,
              toolSquare: widget.toolSquare,
            ),
          )
        else
          _swapCell(
            state,
            slot: slot,
            child: _FreeCell(onOpen: (ctx, key) => _openPicker(ctx, key, slot)),
          ),
    ];
  }
}

/// One agent on the board: tool chip, critter, title, status line.
/// An agent's subagents at work, climbing the tile's right edge: a small
/// critter each, in the session's colour and hat, posed by its tool, the
/// newest on top. Four fit; a "+N" above them counts the rest.
class SubagentLadder extends StatelessWidget {
  const SubagentLadder({
    super.key,
    required this.subagents,
    required this.body,
    required this.cut,
    required this.dim,
    this.hat,
  });

  /// The minis a tile shows before it counts.
  static const max = 4;

  /// The ladder's width on the tile's 170 px design: a 36 px mini.
  static const width = 36.0;

  /// Its gap to the tile's right and bottom edge, clear of the context ring.
  static const inset = 8.0;

  final List<AgentSubagent> subagents;
  final AgentHat? hat;
  final Color body;
  final Color cut;
  final Color dim;

  @override
  Widget build(BuildContext context) {
    final n = subagents.length;
    final workflow = subagents.any((s) => s.type == 'workflow');
    final shown = subagents.reversed.take(max).toList();
    final more = n - shown.length;
    return Semantics(
      label: workflow
          ? 'workflow · $n agent${n == 1 ? '' : 's'}'
          : '$n subagent${n == 1 ? '' : 's'} at work',
      child: Column(
        key: const Key('agent-tile-subagents'),
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          if (more > 0)
            Padding(
              padding: const EdgeInsets.only(right: 8, bottom: 2),
              child: Text('+$more', style: DeckHud.mono(size: 11, color: dim)),
            ),
          for (final (i, s) in shown.indexed)
            Padding(
              padding: EdgeInsets.only(top: i == 0 ? 0 : 3),
              child: CritterSprite(
                pose: critterPoseForTool(s.tool),
                width: width,
                hat: hat,
                body: body,
                cut: cut,
              ),
            ),
        ],
      ),
    );
  }
}

/// A running workflow's phases as a bar: one segment per phase in the
/// script's order, done ones dimmed, the running one lit in the workflow's
/// blue, the rest in the tile's dim.
class WorkflowBar extends StatelessWidget {
  const WorkflowBar({
    super.key,
    required this.workflow,
    required this.dim,
    this.width = 104,
  });

  final AgentWorkflow workflow;
  final Color dim;
  final double width;

  @override
  Widget build(BuildContext context) {
    final i = workflow.current;
    return Semantics(
      label: 'workflow ${workflow.name}: ${workflow.line}',
      child: SizedBox(
        key: const Key('agent-tile-workflow'),
        width: width,
        height: 5,
        child: Row(
          // Stretch: a ColoredBox without a child has no height of its own.
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final (k, p) in workflow.phases.indexed) ...[
              if (k > 0) const SizedBox(width: 3),
              Expanded(
                child: ColoredBox(
                  color: k == i
                      ? DeckHud.workflow
                      : (k < i || (p.done > 0 && p.running == 0))
                      ? DeckHud.workflow.withValues(alpha: 0.45)
                      : dim.withValues(alpha: 0.35),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class AgentTile extends StatefulWidget {
  const AgentTile({
    super.key,
    required this.agent,
    required this.state,
    required this.onOpen,
    this.toolSquare = false,
  });

  final Agent agent;

  /// See [AgentsApp.toolSquare].
  final bool toolSquare;
  final AgentsState state;
  final void Function(BuildContext tileContext, GlobalKey tileKey, Agent a)
  onOpen;

  @override
  State<AgentTile> createState() => _AgentTileState();
}

class _AgentTileState extends State<AgentTile> {
  static const _tileDesign = 170.0;
  final _tileKey = GlobalKey();

  @override
  Widget build(BuildContext context) {
    final a = widget.agent;
    final look = AgentLook.of(a);
    return Pressable(
      key: _tileKey,
      onTap: () => widget.onOpen(context, _tileKey, a),
      builder: (context, pressed) {
        // Pressed = the deck's ink invert.
        final fill = pressed ? DeckHud.ink : look.fill;
        Color c(Color v) => pressed ? DeckHud.bg : v;
        return RepaintBoundary(
          child: CustomPaint(
            // Over the content so the ring never shifts the layout.
            foregroundPainter: pressed
                ? null
                : ContextRingPainter(
                    fraction: a.contextFraction,
                    color: look.ring,
                    track: look.ringTrack,
                  ),
            child: Container(
              color: fill,
              child: Stack(
                children: [
                  if (look.stripes && !pressed)
                    const Positioned.fill(child: HungryStripes())
                  else if (agentWorkflowing(a) && !pressed)
                    Positioned.fill(
                      child: WorkflowTwinkle(
                        key: const Key('agent-tile-twinkle'),
                        seed: a.id,
                      ),
                    ),
                  // The colour of the Claude account it runs in, or (when
                  // asked for) the tool's.
                  if (!pressed && (a.account != null || widget.toolSquare))
                    Positioned(
                      top: 10,
                      right: 10,
                      child: Semantics(
                        label: a.account == null
                            ? null
                            : '${a.account!.name} account',
                        child: Container(
                          key: a.account == null
                              ? const Key('agent-tile-tool')
                              : const Key('agent-tile-account'),
                          width: 8,
                          height: 8,
                          color: a.account == null
                              ? look.chip
                              : hatColor(a.account!.color).$1,
                        ),
                      ),
                    ),
                  // Laid out on the deck's 170 px cell and scaled to the
                  // real one, so any grid size keeps the same composition.
                  // With subagents out, the session steps left to make room
                  // for their ladder up the right edge.
                  Positioned.fill(
                    child: FittedBox(
                      child: SizedBox.square(
                        dimension: _tileDesign,
                        child: Stack(
                          // The column keeps the cell's width: centred.
                          fit: StackFit.expand,
                          children: [
                            if (a.subagents.isNotEmpty)
                              Positioned(
                                right: SubagentLadder.inset,
                                bottom: SubagentLadder.inset,
                                child: SubagentLadder(
                                  subagents: a.subagents,
                                  hat: a.hat,
                                  body: c(look.body),
                                  cut: fill,
                                  dim: c(look.meta),
                                ),
                              ),
                            Padding(
                              padding: EdgeInsets.only(
                                right: a.subagents.isEmpty
                                    ? 0
                                    : SubagentLadder.width +
                                          SubagentLadder.inset,
                              ),
                              child: Column(
                                children: [
                                  const SizedBox(height: 30),
                                  CritterSprite(
                                    pose: critterPoseOf(
                                      a,
                                      inStatus: widget.state.inStatus(a),
                                    ),
                                    celebrateUntil: DateTime.now().add(
                                      critterCelebration -
                                          widget.state.inStatus(a),
                                    ),
                                    afterCelebrate: a.unseen
                                        ? CritterPose.hungry
                                        : CritterPose.idle,
                                    species: critterSpeciesFor(a.tool),
                                    hat: a.hat,
                                    centerBody: true,
                                    width: 92,
                                    body: c(look.body),
                                    celebrateBody: c(DeckHud.accent),
                                    bubble: look.bubble == null
                                        ? null
                                        : c(look.bubble!),
                                    cut: fill,
                                  ),
                                  const SizedBox(height: 14),
                                  _line(
                                    a.title,
                                    DeckHud.mono(
                                      size: 16,
                                      color: c(look.label),
                                    ),
                                  ),
                                  const SizedBox(height: 5),
                                  if (a.workflow case final wf?) ...[
                                    WorkflowBar(
                                      workflow: wf,
                                      dim: c(look.meta),
                                    ),
                                    const SizedBox(height: 5),
                                    _line(
                                      wf.line,
                                      DeckHud.mono(
                                        size: 12.5,
                                        color: DeckHud.workflow,
                                      ).copyWith(letterSpacing: 0.4),
                                    ),
                                  ] else
                                    _line(
                                      agentMetaLine(a, widget.state),
                                      DeckHud.mono(
                                        size: 12.5,
                                        color: c(look.meta),
                                      ).copyWith(letterSpacing: 0.4),
                                    ),
                                ],
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  static Widget _line(String text, TextStyle style) => Padding(
    padding: const EdgeInsets.symmetric(horizontal: 10),
    child: Text(
      text,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
      textAlign: TextAlign.center,
      style: style,
    ),
  );
}

/// What follows the finger while a tile is dragged.
class _DragGhost extends StatelessWidget {
  const _DragGhost({required this.title});

  final String title;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: BoxDecoration(
        color: DeckHud.panel.withValues(alpha: 0.9),
        border: Border.all(color: DeckHud.ink, width: 2),
      ),
      alignment: Alignment.center,
      padding: const EdgeInsets.all(10),
      child: Text(
        title,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        textAlign: TextAlign.center,
        style: DeckHud.mono(size: 16, color: DeckHud.ink),
      ),
    );
  }
}

/// The tile's border as a context gauge: [fraction] of the perimeter, drawn
/// clockwise from the top centre over a faint full-length track.
class ContextRingPainter extends CustomPainter {
  ContextRingPainter({
    required this.fraction,
    required this.color,
    required this.track,
  });

  final double fraction;
  final Color color;
  final Color track;

  static const stroke = 4.0;

  @override
  void paint(Canvas canvas, Size size) {
    const h = stroke / 2;
    final w = size.width, ht = size.height;
    final path = Path()
      ..moveTo(w / 2, h)
      ..lineTo(w - h, h)
      ..lineTo(w - h, ht - h)
      ..lineTo(h, ht - h)
      ..lineTo(h, h)
      ..close();
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = stroke
      ..strokeJoin = StrokeJoin.miter
      ..isAntiAlias = false;
    canvas.drawPath(path, paint..color = track);
    final f = fraction.clamp(0.0, 1.0);
    if (f > 0) {
      final metric = path.computeMetrics().first;
      canvas.drawPath(
        metric.extractPath(0, metric.length * f),
        paint..color = color,
      );
    }
  }

  @override
  bool shouldRepaint(ContextRingPainter old) =>
      old.fraction != fraction || old.color != color || old.track != track;
}

/// Unused slot: near-black block with a faint frame.
/// A free cell: tap it to add a session there.
class _FreeCell extends StatefulWidget {
  const _FreeCell({required this.onOpen});

  final void Function(BuildContext context, GlobalKey key) onOpen;

  @override
  State<_FreeCell> createState() => _FreeCellState();
}

class _FreeCellState extends State<_FreeCell> {
  final _key = GlobalKey();

  @override
  Widget build(BuildContext context) => GestureDetector(
    key: _key,
    behavior: HitTestBehavior.opaque,
    onTap: () => widget.onOpen(context, _key),
    child: const AgentsEmptyTile(),
  );
}

class AgentsEmptyTile extends StatelessWidget {
  const AgentsEmptyTile({super.key});

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: agentsEmptyFill,
        border: Border.all(color: agentsEmptyOutline),
      ),
      child: const SizedBox.expand(),
    );
  }
}

/// One rounded bar per page under the deck: the current one wide and bright,
/// another page's orange when an agent there needs you. Tap one to go there.
class _PageBars extends StatelessWidget {
  const _PageBars({
    required this.pages,
    required this.current,
    required this.hot,
    required this.onTap,
  });

  final int pages;
  final int current;
  final Set<int> hot;
  final ValueChanged<int> onTap;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'page ${current + 1} of $pages',
      child: Row(
        key: const Key('agents-pager'),
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          for (var p = 0; p < pages; p++)
            GestureDetector(
              key: Key('agents-page-$p'),
              behavior: HitTestBehavior.opaque,
              onTap: () => onTap(p),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 6,
                  vertical: 12,
                ),
                child: AnimatedContainer(
                  duration: const Duration(milliseconds: 200),
                  width: p == current ? 56 : 30,
                  height: 8,
                  decoration: BoxDecoration(
                    // Where you are is always bright; other pages turn
                    // orange when an agent there needs you.
                    color: p == current
                        ? DeckHud.ink
                        : (hot.contains(p)
                              ? DeckHud.accent
                              : const Color(0xFF2A2925)),
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class _OfflineChip extends StatelessWidget {
  const _OfflineChip({this.problem});

  final String? problem;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('agents-offline'),
      constraints: const BoxConstraints(maxWidth: 640),
      padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 9),
      decoration: BoxDecoration(
        color: DeckHud.panel,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: DeckHud.accent, width: 1.5),
      ),
      child: Text(
        'offline · retrying${problem == null ? '' : ' · $problem'}',
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: DeckHud.rm(14, DeckHud.accent),
      ),
    );
  }
}

/// Centred critter + a few lines (connecting, empty fleet).
class _Notice extends StatelessWidget {
  const _Notice({
    required this.pose,
    required this.title,
    this.lines = const [],
    this.codeLine,
  });

  final CritterPose pose;
  final String title;
  final List<String> lines;

  /// Index into [lines] rendered as a command (ink, Roboto Mono).
  final int? codeLine;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 48),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            CritterSprite(
              pose: pose,
              width: 126,
              body: DeckHud.dim,
              cut: DeckHud.bg,
              centerBody: true,
            ),
            const SizedBox(height: 28),
            Text(
              title,
              textAlign: TextAlign.center,
              style: DeckHud.rm(28, DeckHud.ink, weight: 700),
            ),
            for (final (i, l) in lines.indexed) ...[
              const SizedBox(height: 12),
              Text(
                l,
                textAlign: TextAlign.center,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: i == codeLine
                    ? DeckHud.rm(20, DeckHud.accent)
                    : DeckHud.mono(size: 16, color: DeckHud.dim),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Not paired yet: how to get the three values onto the panel.
class AgentsPairingCard extends StatelessWidget {
  const AgentsPairingCard({super.key, this.problem});

  /// Why saved values were not usable (e.g. a malformed agents.json).
  final String? problem;

  @override
  Widget build(BuildContext context) {
    Widget step(String n, List<InlineSpan> text) => Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 34,
            child: Text(n, style: DeckHud.rm(20, DeckHud.accent, weight: 700)),
          ),
          Expanded(
            child: Text.rich(
              TextSpan(children: text),
              style: DeckHud.mono(
                size: 17,
                color: DeckHud.ink,
              ).copyWith(height: 1.4),
            ),
          ),
        ],
      ),
    );
    TextSpan code(String s) =>
        TextSpan(text: s, style: DeckHud.rm(17, DeckHud.accent));
    return Padding(
      padding: const EdgeInsets.all(8),
      child: Container(
        key: const Key('agents-pairing'),
        color: DeckHud.panel,
        padding: const EdgeInsets.fromLTRB(40, 44, 40, 36),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const CritterSprite(
              pose: CritterPose.wait,
              width: 90,
              body: DeckHud.ink,
              cut: DeckHud.panel,
            ),
            const SizedBox(height: 26),
            Text(
              'pair with your mac',
              style: DeckHud.rm(30, DeckHud.ink, weight: 700),
            ),
            const SizedBox(height: 10),
            Text(
              'the agents board needs a pairing bundle from your mac: '
              'its address, its certificate and a key for this panel.',
              style: DeckHud.mono(
                size: 16,
                color: DeckHud.dim,
              ).copyWith(height: 1.4),
            ),
            const SizedBox(height: 34),
            step('1', [
              const TextSpan(text: 'on the mac, run '),
              code('agentctl pair desk-panel --bundle -'),
            ]),
            step('2', [
              const TextSpan(text: 'open the panel setup page '),
              code(agentsSetupUrl()),
            ]),
            step('3', [
              const TextSpan(text: 'paste the whole output into '),
              const TextSpan(
                text: 'agents',
                style: TextStyle(color: DeckHud.accent),
              ),
              const TextSpan(text: ' and save'),
            ]),
            const Spacer(),
            Text(
              problem ??
                  'the board connects by itself once the bundle is saved.',
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: DeckHud.mono(
                size: 14,
                color: problem == null ? DeckHud.dim : DeckHud.accent,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
