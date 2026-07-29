import 'package:flutter/material.dart';

import '../../core/storage/secure_storage.dart';
import '../../features/authentication/data/datasources/google_sign_in_api.dart';
import '../../features/notifications/notification_service.dart';
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
  const QueelAppBar({super.key, required this.title});

  final String title;

  @override
  Size get preferredSize => const Size.fromHeight(kToolbarHeight);

  @override
  Widget build(BuildContext context) {
    return AppBar(
      title: Text(title),
      actions: [
        PopupMenuButton<String>(
          onSelected: (value) => _select(context, value),
          itemBuilder: (_) => const [
            PopupMenuItem(value: AppRouter.subscriptions, child: Text('Mes abonnements')),
            // Last, and set apart: it's the one entry that undoes the
            // session rather than moving around inside it.
            PopupMenuDivider(),
            PopupMenuItem(value: _logoutAction, child: Text('Déconnexion')),
          ],
        ),
      ],
    );
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
