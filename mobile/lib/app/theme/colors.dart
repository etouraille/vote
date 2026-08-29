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

  /// A vote cast, a text followed — something the reader has settled.
  static const settled = Color(0xFF34C759);

  /// A round in progress, a label: something live, awaiting a decision.
  static const pending = Color(0xFFF59E0B);

  /// A wording struck out, an action that cannot be taken back.
  static const removed = Color(0xFFEF4444);
}
