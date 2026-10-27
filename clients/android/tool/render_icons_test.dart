// Renders the launcher icons from the deck's own critter sprite, so the icon
// is the same pixel art the board draws. Not a test of anything; it runs in
// the test harness because that is where a Flutter widget can be painted
// headlessly:
//
//   flutter test tool/render_icons_test.dart
//
// The icon is the critter awake, in its propeller hat, black on the deck's
// orange. Writes, per density, android/app/src/main/res/mipmap-*/ic_launcher.png
// (the legacy icon, the whole square), ic_launcher_foreground.png (the
// adaptive icon's layer, transparent around the critter; the orange is the
// background layer) and ic_launcher_monochrome.png (the silhouette for themed
// icons, which take only its alpha).
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

  const hat = AgentHat(shape: 'propeller', color: 'blue');

  /// The critter awake (eyes open, no bubble) in its hat, the hat's rows
  /// lifting it a little so body and hat together sit centred.
  Widget critter(
    double width, {
    required Color body,
    required Color cut,
    double lift = 0,
  }) => Center(
    child: Transform.translate(
      offset: Offset(0, lift),
      child: CritterSprite(
        pose: CritterPose.run,
        width: width,
        body: body,
        cut: cut,
        hat: hat,
        showBubble: false,
        centerBody: true,
      ),
    ),
  );

  testWidgets('render the launcher icons', (tester) async {
    // Legacy icon (Android 7): a 48 dp square, black critter on orange.
    await render(
      tester,
      name: 'ic_launcher',
      dp: 48,
      child: ColoredBox(
        color: DeckHud.accent,
        child: critter(54, body: DeckHud.bg, cut: DeckHud.accent, lift: 2),
      ),
    );
    // Adaptive foreground (Android 8+): 108 dp, the inner 66 dp always shown;
    // the body spans 62 of them, the hat on top. The ground is @color/deck_accent.
    await render(
      tester,
      name: 'ic_launcher_foreground',
      dp: 108,
      child: critter(80, body: DeckHud.bg, cut: DeckHud.accent, lift: 4),
    );
    // Monochrome (Android 13 themed icons): the system takes the alpha only.
    await render(
      tester,
      name: 'ic_launcher_monochrome',
      dp: 108,
      child: critter(
        80,
        body: const Color(0xFFFFFFFF),
        cut: const Color(0x00000000),
        lift: 4,
      ),
    );
  });
}
