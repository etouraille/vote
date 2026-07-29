import 'package:flutter/material.dart';

import '../features/authentication/presentation/pages/login_page.dart';
import '../features/search/presentation/pages/search_page.dart';
import '../features/subscriptions/presentation/pages/subscriptions_page.dart';

/// Route names and generation in one place, instead of scattering route
/// strings across the app.
class AppRouter {
  AppRouter._();

  static const String login = '/login';
  static const String search = '/search';
  static const String subscriptions = '/subscriptions';

  /// Every route is built with `settings:` forwarded, so the pushed route
  /// keeps its own name — without it ModalRoute.of(context).settings.name
  /// is null everywhere, and anything asking "which screen am I on?"
  /// (see QueelAppBar) silently gets no answer.
  static Route<dynamic> onGenerateRoute(RouteSettings settings) {
    switch (settings.name) {
      case login:
        return MaterialPageRoute(settings: settings, builder: (_) => const LoginPage());
      case search:
        return MaterialPageRoute(settings: settings, builder: (_) => const SearchPage());
      case subscriptions:
        return MaterialPageRoute(settings: settings, builder: (_) => const SubscriptionsPage());
      default:
        throw ArgumentError('Unknown route: ${settings.name}');
    }
  }
}
