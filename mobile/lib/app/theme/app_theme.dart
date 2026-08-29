import 'package:flutter/material.dart';

import 'colors.dart';

class AppTheme {
  AppTheme._();

  static ThemeData get light {
    // fromSeed for the tones nobody names — surfaces, containers, outlines.
    // The primary is pinned to the seed itself rather than to the tone
    // Material derives from it: it is the front's --color-brand to the
    // byte, and a derived tone would be a near miss, which reads worse
    // than an honest difference.
    final scheme = ColorScheme.fromSeed(
      seedColor: AppColors.seed,
    ).copyWith(
      primary: AppColors.seed,
      onPrimary: Colors.white,
      // The base red, not the ink: error here fills the unread badge, and
      // the front fills its own badge with the same bg-removed. Error text
      // takes AppColors.removedInk at the call site, as text-removed-ink
      // does there.
      error: AppColors.removed,
      onError: Colors.white,
    );

    return ThemeData(
      colorScheme: scheme,

      // The bar carries the identity, so it carries the colour. Material's
      // default leaves it the same near-white as the page under it, which
      // makes every screen of this app look like every screen of any
      // other.
      appBarTheme: AppBarTheme(
        backgroundColor: scheme.primary,
        foregroundColor: scheme.onPrimary,
        elevation: 0,
        titleTextStyle: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600),
      ),

      // Tinted rather than white: the icon's paper sits on colour, and a
      // card that ignores the scheme reads as pasted onto it.
      cardTheme: CardThemeData(
        color: scheme.surfaceContainerLow,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(color: scheme.outlineVariant),
        ),
      ),

      chipTheme: ChipThemeData(
        backgroundColor: scheme.surfaceContainerHighest,
        selectedColor: scheme.primaryContainer,
        side: BorderSide(color: scheme.outlineVariant),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      ),

      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: scheme.surfaceContainerLowest,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: scheme.outlineVariant),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(10),
          borderSide: BorderSide(color: scheme.outlineVariant),
        ),
      ),

      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
        ),
      ),

      dividerTheme: DividerThemeData(color: scheme.outlineVariant, space: 1),
    );
  }
}
