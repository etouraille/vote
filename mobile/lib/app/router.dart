import 'package:flutter/material.dart';

import '../features/articles/presentation/pages/articles_page.dart';
import '../features/authentication/presentation/pages/login_page.dart';
import '../features/notifications/presentation/pages/notifications_page.dart';
import '../features/subscriptions/presentation/pages/subscriptions_page.dart';

/// Route names and generation in one place, instead of scattering route
/// strings across the app.
class AppRouter {
  AppRouter._();

  /// Lets code outside the widget tree navigate — specifically a tapped
  /// notification, which is handled by NotificationService and has no
  /// BuildContext of its own to push from.
  static final navigatorKey = GlobalKey<NavigatorState>();

  static const String login = '/login';
  static const String articles = '/articles';
  static const String subscriptions = '/subscriptions';
  static const String notifications = '/notifications';

  /// Every route is built with `settings:` forwarded, so the pushed route
  /// keeps its own name — without it ModalRoute.of(context).settings.name
  /// is null everywhere, and anything asking "which screen am I on?"
  /// (see QueelAppBar) silently gets no answer.
  static Route<dynamic> onGenerateRoute(RouteSettings settings) {
    switch (settings.name) {
      case login:
        return MaterialPageRoute(settings: settings, builder: (_) => const LoginPage());
      case articles:
        return MaterialPageRoute(settings: settings, builder: (_) => const ArticlesPage());
      case subscriptions:
        return MaterialPageRoute(settings: settings, builder: (_) => const SubscriptionsPage());
      case notifications:
        return MaterialPageRoute(settings: settings, builder: (_) => const NotificationsPage());
      default:
        throw ArgumentError('Unknown route: ${settings.name}');
    }
  }
}
