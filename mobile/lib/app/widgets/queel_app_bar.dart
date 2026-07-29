import 'package:flutter/material.dart';

import '../router.dart';

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
          onSelected: (value) => _open(context, value),
          itemBuilder: (_) => const [
            PopupMenuItem(value: AppRouter.subscriptions, child: Text('Mes abonnements')),
          ],
        ),
      ],
    );
  }

  void _open(BuildContext context, String route) {
    // Selecting the screen you're already on would otherwise stack a second
    // identical copy, which only shows up as a back button that appears to
    // do nothing.
    if (ModalRoute.of(context)?.settings.name == route) return;
    Navigator.of(context).pushNamed(route);
  }
}
