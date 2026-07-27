import 'package:flutter/material.dart';

import 'colors.dart';

class AppTheme {
  AppTheme._();

  static ThemeData get light => ThemeData(colorScheme: ColorScheme.fromSeed(seedColor: AppColors.seed));
}
