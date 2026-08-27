import 'package:flutter/material.dart';

import '../../core/storage/secure_storage.dart';
import '../../features/authentication/data/datasources/google_sign_in_api.dart';
import '../../features/notifications/notification_service.dart';
import '../../features/notifications/presentation/widgets/notifications_bell.dart';
import '../../features/notifications/unread_notifications.dart';
import '../router.dart';

/// Menu entry that signs out rather than navigating. Not a route, so it
/// can't collide with one — the menu keys on this value alone.
const _logoutAction = 'logout';

/// The app bar every signed-in screen uses, so the overflow menu is reached
/// from anywhere rather than only from the search page.
///
/// Deliberately not used by the login page: its entries all lead to
/// screens that require a session, so offering them to someone who hasn't
/// signed in yet would only produce failed requests.
class QueelAppBar extends StatelessWidget implements PreferredSizeWidget {
  const QueelAppBar({
    super.key,
    required this.title,
    this.actions = const [],
    this.showNotificationsBell = true,
  });

  final String title;

  /// Screen-specific buttons, placed before the bell and the overflow menu
  /// so the two fixtures every screen shares keep the same position
  /// wherever you are.
  final List<Widget> actions;

  /// Set false by the inbox screen itself — a bell that reopens the screen
  /// you are already reading is at best a no-op, and its badge would
  /// contradict the list right under it as entries are read.
  final bool showNotificationsBell;

  @override
  Size get preferredSize => const Size.fromHeight(kToolbarHeight);

  @override
  Widget build(BuildContext context) {
    return AppBar(
      title: Text(title),
      actions: [
        ...actions,
        if (showNotificationsBell) const NotificationsBell(),
        PopupMenuButton<String>(
          onSelected: (value) => _select(context, value),
          // Not const, and no listener either: itemBuilder runs when the
          // menu is opened, so reading the count here is enough to have it
          // current every time it's shown.
          itemBuilder: (_) => [
            PopupMenuItem(
              value: AppRouter.notifications,
              // A second way in, alongside the bell — the bell carries the
              // badge, this carries the name, and someone looking for the
              // feature by name finds it where every other destination is.
              child: Text(_notificationsLabel()),
            ),
            const PopupMenuItem(value: AppRouter.subscriptions, child: Text('Mes abonnements')),
            // Last, and set apart: it's the one entry that undoes the
            // session rather than moving around inside it.
            const PopupMenuDivider(),
            const PopupMenuItem(value: _logoutAction, child: Text('Déconnexion')),
          ],
        ),
      ],
    );
  }

  static String _notificationsLabel() {
    final unread = UnreadNotifications.count.value;
    return unread == 0 ? 'Notifications' : 'Notifications ($unread)';
  }

  void _select(BuildContext context, String value) {
    if (value == _logoutAction) {
      _logout(context);
      return;
    }

    // Selecting the screen you're already on would otherwise stack a second
    // identical copy, which only shows up as a back button that appears to
    // do nothing.
    if (ModalRoute.of(context)?.settings.name == value) return;
    Navigator.of(context).pushNamed(value);
  }

  Future<void> _logout(BuildContext context) async {
    final navigator = Navigator.of(context);

    // Before clearing the session, not after: the api scopes the deletion
    // to the caller, so it has nothing left to match on once the bearer
    // token is gone.
    await NotificationService.unregisterDevice();

    await SecureStorage.clear();

    // Or the next person to sign in on this device inherits a badge
    // counting someone else's notifications.
    UnreadNotifications.clear();

    // Also drop the plugin's cached Google account, or "Continuer avec
    // Google" would sign the same person straight back in without ever
    // showing the picker — which doesn't look like having signed out.
    // Best-effort: failing to clear it must not keep the user signed in.
    try {
      await GoogleSignInApi.signOut();
    } catch (_) {}

    // pushNamedAndRemoveUntil, not pushReplacement: signing out has to drop
    // every screen behind this one too, or the back button walks straight
    // back into a session that no longer exists.
    navigator.pushNamedAndRemoveUntil(AppRouter.login, (_) => false);
  }
}
