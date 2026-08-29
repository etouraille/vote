import 'package:flutter/material.dart';

/// The palette, taken from the app icon (lib/picture/queel_splash.png) so
/// the app looks like the thing the launcher shows.
///
/// The icon is a blue-to-violet gradient carrying a white sheet of paper,
/// marked up in green, orange and violet — an edit under review. Those
/// three are the colours this app already needed for its own states, which
/// is why they are lifted rather than invented: what the icon says about
/// the product is what the screens say about a text.
class AppColors {
  AppColors._();

  /// Midway along the icon's gradient — the hue the whole scheme derives
  /// from. Neither end alone: the blue reads as any other utility app, the
  /// violet as heavier than the product is.
  static const seed = Color(0xFF6366F1);

  /// The gradient's ends, for the surfaces that carry it — the sign-in
  /// screen and the top of the article list.
  static const gradientStart = Color(0xFF3B82F6);
  static const gradientEnd = Color(0xFF8B5CF6);

  /// The marks on the icon's sheet of paper, and what they mean here.
  ///
  /// Kept as named intentions rather than raw colours at the call sites:
  /// the same green means "settled" on a notification and on a
  /// subscription, and naming it once is what keeps them the same green.

  /// Three tokens per intention rather than one, because a colour picked to
  /// be seen as a shape is not readable as a word: the base fills (buttons,
  /// dots, badges), the tint backs a run of text, the ink is the text
  /// itself. Every ink clears 4.5:1 on both white and its own tint — the
  /// bare green is 2.2:1 on white, which is a label nobody reads.
  ///
  /// front/src/styles.css declares the same twelve values as Tailwind
  /// tokens. Changing one side without the other is what makes two clients
  /// look like two products.

  /// A vote cast, a text followed — something the reader has settled.
  static const settled = Color(0xFF34C759);
  static const settledInk = Color(0xFF207B37);
  static const settledTint = Color(0xFFE1F7E6);

  /// A round in progress, a label: something live, awaiting a decision.
  static const pending = Color(0xFFF59E0B);
  static const pendingInk = Color(0xFF986207);
  static const pendingTint = Color(0xFFFEF0DA);

  /// A wording struck out, an error, an action that cannot be taken back.
  static const removed = Color(0xFFEF4444);
  static const removedInk = Color(0xFFBF3636);
  static const removedTint = Color(0xFFFDE3E3);
}
