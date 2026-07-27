// Generates lib/picture/queel_splash.png from lib/picture/queel.png: the
// source icon is opaque edge-to-edge, but Android 12+'s splash screen API
// always clips its icon into a circle — with no transparent margin, that
// circle visibly crops the logo's corners. This pads queel.png onto a
// transparent canvas at a reduced scale so the crop circle falls entirely
// within the transparent margin instead, making it invisible against
// flutter_native_splash's background color.
//
// Re-run with `dart run tool/gen_splash_icon.dart` after changing queel.png.
import 'dart:io';

import 'package:image/image.dart' as img;

void main() {
  final source = img.decodePng(File('lib/picture/queel.png').readAsBytesSync())!;

  const canvasSize = 1254;
  // Android's safe zone for a splash/adaptive icon is its inner ~66%; this
  // stays comfortably inside that so the circular crop never touches it.
  const iconScale = 0.55;
  final iconSize = (canvasSize * iconScale).round();

  final resizedIcon = img.copyResize(source, width: iconSize, height: iconSize, interpolation: img.Interpolation.average);

  final canvas = img.Image(width: canvasSize, height: canvasSize, numChannels: 4);
  img.fill(canvas, color: img.ColorRgba8(0, 0, 0, 0));

  final offset = (canvasSize - iconSize) ~/ 2;
  img.compositeImage(canvas, resizedIcon, dstX: offset, dstY: offset);

  File('lib/picture/queel_splash.png').writeAsBytesSync(img.encodePng(canvas));
  stdout.writeln('Wrote lib/picture/queel_splash.png (${canvas.width}x${canvas.height}, icon at ${(iconScale * 100).round()}% scale)');
}
