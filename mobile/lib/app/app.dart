import 'package:flutter/material.dart';

import 'router.dart';
import 'theme/app_theme.dart';

class QueelApp extends StatelessWidget {
  const QueelApp({super.key, required this.initialPage});

  /// Where main() decided to land: SearchPage if a still-valid session was
  /// already in SecureStorage, LoginPage otherwise. A concrete widget
  /// rather than a route name — MaterialApp.initialRoute would make
  /// Flutter also try to generate "/" as an ancestor route first (its
  /// default behavior for any named initial route other than "/" itself),
  /// which AppRouter.onGenerateRoute doesn't define.
  final Widget initialPage;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Queel',
      theme: AppTheme.light,
      home: initialPage,
      onGenerateRoute: AppRouter.onGenerateRoute,
    );
  }
}
