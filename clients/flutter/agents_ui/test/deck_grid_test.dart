import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  List<Widget> cells(int n) => [
    for (var i = 0; i < n; i++) SizedBox(key: ValueKey('cell-$i')),
  ];

  Future<void> pump(WidgetTester tester, Widget grid, Size size) {
    return tester.pumpWidget(
      Directionality(
        textDirection: TextDirection.ltr,
        child: Center(
          child: SizedBox(width: size.width, height: size.height, child: grid),
        ),
      ),
    );
  }

  Rect cell(WidgetTester tester, int i) =>
      tester.getRect(find.byKey(ValueKey('cell-$i')));

  testWidgets('4×4 by default: 16 square cells, four per row', (tester) async {
    await pump(tester, DeckGrid(cells: cells(16)), const Size(704, 704));
    expect(cell(tester, 3).top, cell(tester, 0).top);
    expect(cell(tester, 4).top, greaterThan(cell(tester, 0).top));
    expect(cell(tester, 0).width, cell(tester, 0).height);
    expect(cell(tester, 15).bottom, lessThanOrEqualTo(704));
  });

  testWidgets('3 columns: the 16 slots in six rows, cells still square', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(526, 1060);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await pump(
      tester,
      DeckGrid(cells: cells(16), columns: 3, rows: 6),
      const Size(526, 1060),
    );
    expect(cell(tester, 2).top, cell(tester, 0).top);
    expect(cell(tester, 3).top, greaterThan(cell(tester, 2).top));
    expect(cell(tester, 3).left, cell(tester, 0).left);
    expect(cell(tester, 15).top, greaterThan(cell(tester, 12).top));
    expect(cell(tester, 0).width, 170);
    expect(cell(tester, 0).height, 170);
  });

  testWidgets('AgentsApp passes its columns on to the grid', (tester) async {
    tester.view.physicalSize = const Size(526, 1060);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      WidgetsApp(
        color: const Color(0xFF000000),
        builder: (context, _) => Overlay(
          initialEntries: [
            OverlayEntry(
              builder: (_) => AgentsApp(
                source: FakeAgentsSource.demo(),
                animate: false,
                columns: 3,
              ),
            ),
          ],
        ),
      ),
    );
    await tester.pump();
    final grid = tester.widget<DeckGrid>(find.byType(DeckGrid));
    expect((grid.columns, grid.rows), (3, 6));
  });
}
