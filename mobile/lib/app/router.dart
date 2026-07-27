import 'package:flutter/material.dart';

import '../features/home/presentation/pages/home_page.dart';

/// Route names and generation in one place — just home today, but this is
/// where each features/*/presentation/pages entry gets wired in as it's
/// added, instead of scattering route strings across the app.
class AppRouter {
  AppRouter._();

  static const String home = '/';

  static Route<dynamic> onGenerateRoute(RouteSettings settings) {
    switch (settings.name) {
      case home:
        return MaterialPageRoute(builder: (_) => const HomePage());
      default:
        throw ArgumentError('Unknown route: ${settings.name}');
    }
  }
}
