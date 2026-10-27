// Renders the launcher icons from the deck's own critter sprite, so the icon
// is the same pixel art the board draws. Not a test of anything; it runs in
// the test harness because that is where a Flutter widget can be painted
// headlessly:
//
//   flutter test tool/render_icons_test.dart
//
// Writes, per density, android/app/src/main/res/mipmap-*/ic_launcher.png
// (the legacy icon: critter on the board's black), ic_launcher_foreground.png
// (the adaptive icon's layer, transparent around the critter) and
// ic_launcher_monochrome.png (white, for themed icons).
import 'dart:io';
import 'dart:ui' as ui;

import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

const _densities = {
  'mdpi': 1.0,
  'hdpi': 1.5,
  'xhdpi': 2.0,
  'xxhdpi': 3.0,
  'xxxhdpi': 4.0,
};

void main() {
  final res = Directory('android/app/src/main/res');

  Future<void> render(
    WidgetTester tester, {
    required String name,
    required double dp,
    required Widget child,
  }) async {
    for (final MapEntry(key: density, value: scale) in _densities.entries) {
      tester.view.physicalSize = Size(dp * scale, dp * scale);
      tester.view.devicePixelRatio = scale;
      final key = GlobalKey();
      await tester.pumpWidget(
        RepaintBoundary(
          key: key,
          child: SizedBox(width: dp, height: dp, child: child),
        ),
      );
      await tester.pump();
      final boundary = tester.renderObject<RenderRepaintBoundary>(
        find.byKey(key),
      );
      final png = await tester.runAsync(() async {
        final image = await boundary.toImage(pixelRatio: scale);
        return image.toByteData(format: ui.ImageByteFormat.png);
      });
      final file = File('${res.path}/mipmap-$density/$name.png');
      file.parent.createSync(recursive: true);
      file.writeAsBytesSync(png!.buffer.asUint8List());
    }
    tester.view.reset();
  }

  Widget critter(double width, {required Color body, required Color cut}) =>
      Center(
        child: CritterSprite(
          pose: CritterPose.idle,
          width: width,
          body: body,
          cut: cut,
          showBubble: false,
          centerBody: true,
        ),
      );

  testWidgets('render the launcher icons', (tester) async {
    // Legacy icon (Android 7): a 48 dp square, the critter's body at about 62 %.
    await render(
      tester,
      name: 'ic_launcher',
      dp: 48,
      child: ColoredBox(
        color: DeckHud.bg,
        child: critter(60, body: DeckHud.accent, cut: DeckHud.bg),
      ),
    );
    // Adaptive foreground (Android 8+): 108 dp, the inner 66 dp always shown (the body spans 55).
    await render(
      tester,
      name: 'ic_launcher_foreground',
      dp: 108,
      child: critter(110, body: DeckHud.accent, cut: DeckHud.bg),
    );
    // Monochrome (Android 13 themed icons): the silhouette in white.
    await render(
      tester,
      name: 'ic_launcher_monochrome',
      dp: 108,
      child: critter(
        110,
        body: const Color(0xFFFFFFFF),
        cut: const Color(0x00000000),
      ),
    );
  });
}
