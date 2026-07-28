import 'package:flutter/material.dart';

import '../features/authentication/presentation/pages/login_page.dart';
import '../features/search/presentation/pages/search_page.dart';

/// Route names and generation in one place, instead of scattering route
/// strings across the app.
class AppRouter {
  AppRouter._();

  static const String login = '/login';
  static const String search = '/search';

  static Route<dynamic> onGenerateRoute(RouteSettings settings) {
    switch (settings.name) {
      case login:
        return MaterialPageRoute(builder: (_) => const LoginPage());
      case search:
        return MaterialPageRoute(builder: (_) => const SearchPage());
      default:
        throw ArgumentError('Unknown route: ${settings.name}');
    }
  }
}
