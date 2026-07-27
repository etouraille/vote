import 'package:flutter/material.dart';

import 'router.dart';
import 'theme/app_theme.dart';

class QueelApp extends StatelessWidget {
  const QueelApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Queel',
      theme: AppTheme.light,
      initialRoute: AppRouter.home,
      onGenerateRoute: AppRouter.onGenerateRoute,
    );
  }
}
