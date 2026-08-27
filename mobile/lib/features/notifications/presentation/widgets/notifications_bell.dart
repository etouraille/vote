import 'package:flutter/material.dart';

import '../../../../app/router.dart';
import '../../unread_notifications.dart';

/// The app bar's way into the inbox, with the unread count on it.
///
/// It refreshes on two occasions and no others: when it first appears, and
/// when the inbox screen it opened is popped. There is deliberately no
/// polling — a push already tells the app when something happened (see
/// NotificationService, which bumps the counter on arrival), so a timer
/// would only spend battery re-asking a question already answered.
class NotificationsBell extends StatefulWidget {
  const NotificationsBell({super.key});

  @override
  State<NotificationsBell> createState() => _NotificationsBellState();
}

class _NotificationsBellState extends State<NotificationsBell> {
  @override
  void initState() {
    super.initState();
    // Unawaited and best-effort: the app bar must build now, not after a
    // round trip, and a count that failed to load simply stays as it was.
    UnreadNotifications.refresh();
  }

  Future<void> _open() async {
    await Navigator.of(context).pushNamed(AppRouter.notifications);
    // The inbox sets the count from its own listing while it's open, but
    // anything that arrived meanwhile is only reflected by asking again.
    await UnreadNotifications.refresh(force: true);
  }

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<int>(
      valueListenable: UnreadNotifications.count,
      builder: (context, unread, icon) {
        return IconButton(
          onPressed: _open,
          tooltip: unread == 0 ? 'Notifications' : '$unread notification(s) non lue(s)',
          // Badge draws nothing at all when isLabelVisible is false, so the
          // icon keeps its exact position whether or not there's a count —
          // no shifting app bar the moment a notification arrives.
          icon: Badge(
            isLabelVisible: unread > 0,
            // Past 99 the exact figure stops being information and starts
            // being a wide badge.
            label: Text(unread > 99 ? '99+' : '$unread'),
            child: icon,
          ),
        );
      },
      // Built once and handed to every rebuild: the icon itself never
      // depends on the count.
      child: const Icon(Icons.notifications_outlined),
    );
  }
}
