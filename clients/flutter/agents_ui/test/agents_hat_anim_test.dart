import 'package:agents_deck_ui/agents_deck_ui.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('every hat cycles through its frames while working', () {
    for (final shape in hatShapes) {
      final art = hatArt[shape]!;
      expect(art.critter.length, art.onigiri.length, reason: shape);
      // One full loop, sampled every clock tick (50 ms).
      final ticks = art.ms * art.critter.length ~/ 50 + 1;
      final seen = {
        for (var t = 0; t < ticks; t++)
          critterHatFrame(shape, CritterPose.read, t),
      };
      expect(seen, {
        for (var i = 0; i < art.critter.length; i++) i,
      }, reason: shape);
    }
  });

  test('hats stay still unless the agent is working', () {
    for (final pose in [
      CritterPose.idle,
      CritterPose.wait,
      CritterPose.error,
      CritterPose.hungry,
      CritterPose.celebrate,
      CritterPose.starting,
      CritterPose.exited,
    ]) {
      for (var t = 0; t < 100; t++) {
        expect(critterHatFrame('propeller', pose, t), -1, reason: '$pose');
      }
    }
    expect(critterHatFrame('fedora', CritterPose.run, 7), -1);
  });

  test('the propeller spins fast, the chef toque puffs slowly', () {
    // 120 ms frames: the blades change within three ticks.
    expect(critterHatFrame('propeller', CritterPose.bash, 0), 0);
    expect(critterHatFrame('propeller', CritterPose.bash, 3), 1);
    // 450 ms frames.
    expect(critterHatFrame('chef', CritterPose.web, 8), 0);
    expect(critterHatFrame('chef', CritterPose.web, 9), 1);
  });

  test('the bandana is tied on the left, still and while working', () {
    final art = hatArt['bandana']!;
    for (final f in [art.critterStill, ...art.critter, art.onigiriStill]) {
      expect(f.extras, isNotEmpty);
      expect(f.extras.every((e) => e.$1 <= 2), isTrue);
      expect(f.rows.every((r) => !r.contains('b')), isTrue);
    }
  });
}
