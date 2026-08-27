import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../../search/presentation/pages/text_detail_page.dart';
import '../../../vote/presentation/pages/vote_page.dart';
import '../../data/datasources/notification_api.dart';
import '../../data/models/app_notification.dart';
import '../../notification_types.dart';
import '../../unread_notifications.dart';

/// What push already delivered, readable after the fact.
///
/// Its reason for existing is that a push is seen once or not at all: it is
/// dismissed, or it arrives while the phone is face down, or the app is
/// reinstalled. The inbox is the same events kept server-side, so nothing
/// is lost with the notification drawer.
class NotificationsPage extends StatefulWidget {
  const NotificationsPage({super.key});

  @override
  State<NotificationsPage> createState() => _NotificationsPageState();
}

class _NotificationsPageState extends State<NotificationsPage> {
  List<AppNotification>? _notifications;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final page = await NotificationApi.list();
      // The badge is refreshed from the same response rather than by a
      // separate call: the two can't disagree if they come from one read.
      UnreadNotifications.set(page.unread);
      if (mounted) {
        setState(() {
          _notifications = page.notifications;
          _error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Chargement impossible.');
    }
  }

  /// Applies a read-state change locally, then sends it.
  ///
  /// Optimistic on purpose: marking read is not the point of the tap — the
  /// point is opening the text — and waiting on the round trip would stall
  /// that behind a request whose outcome nobody is watching. A failure
  /// rolls the row back so the list never claims a state the server
  /// doesn't hold.
  Future<void> _setRead(AppNotification notification, bool read) async {
    if (notification.read == read) return;

    _replace(notification.copyWith(read: read));
    UnreadNotifications.set(UnreadNotifications.count.value + (read ? -1 : 1));

    try {
      await NotificationApi.setRead(notification.id, read);
    } catch (_) {
      _replace(notification);
      UnreadNotifications.set(UnreadNotifications.count.value + (read ? 1 : -1));
    }
  }

  void _replace(AppNotification notification) {
    if (!mounted) return;
    setState(() {
      final list = _notifications;
      if (list == null) return;
      final index = list.indexWhere((item) => item.id == notification.id);
      if (index != -1) list[index] = notification;
    });
  }

  Future<void> _markAllRead() async {
    final previous = _notifications;
    if (previous == null || previous.isEmpty) return;

    setState(() {
      _notifications = previous.map((item) => item.copyWith(read: true)).toList();
    });
    UnreadNotifications.clear();

    try {
      await NotificationApi.markAllRead();
    } catch (_) {
      // Re-reading is better than restoring the old list here: a bulk
      // acknowledge may have partly applied, and only the server knows
      // what actually stuck.
      await _load();
    }
  }

  void _open(AppNotification notification) {
    // Reading it is what marks it read — no separate gesture, and no way
    // to end up with an inbox full of entries already acted on.
    _setRead(notification, true);

    if (!notification.opensText) return;
    final textId = notification.textId!;

    Navigator.of(context).push(MaterialPageRoute(
      // An edit awaiting votes opens straight onto the round; anything
      // else opens the text, since there may be nothing to vote on. Same
      // rule a tapped push notification follows (see NotificationService),
      // read from the same place so the two can't drift apart.
      builder: (_) => NotificationTypes.opensVote(notification.type)
          ? VotePage(textId: textId)
          : TextDetailPage(textId: textId),
    ));
  }

  @override
  Widget build(BuildContext context) {
    final notifications = _notifications;

    return Scaffold(
      appBar: QueelAppBar(
        title: 'Notifications',
        showNotificationsBell: false,
        // Only offered when it would do something — an "all read" button
        // over an already-read inbox is a button that can only disappoint.
        actions: [
          if (notifications != null && notifications.any((item) => !item.read))
            IconButton(
              onPressed: _markAllRead,
              icon: const Icon(Icons.done_all),
              tooltip: 'Tout marquer comme lu',
            ),
        ],
      ),
      body: switch ((notifications, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final items?, _) when items.isEmpty => RefreshIndicator(
            onRefresh: _load,
            // A ListView rather than a bare Center: pull-to-refresh needs
            // something scrollable under it, and an empty Center isn't.
            child: ListView(
              children: const [
                Padding(
                  padding: EdgeInsets.symmetric(horizontal: 24, vertical: 64),
                  child: Text(
                    'Aucune notification pour le moment.\nVous serez prévenu ici quand un texte que vous suivez sera modifié.',
                    textAlign: TextAlign.center,
                  ),
                ),
              ],
            ),
          ),
        (final items?, _) => RefreshIndicator(
            onRefresh: _load,
            child: ListView.separated(
              itemCount: items.length,
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (_, index) => _NotificationTile(
                notification: items[index],
                onTap: () => _open(items[index]),
                // Long press is the way back: without it, reading an entry
                // by accident loses it in a list with no other marker.
                onToggleRead: () => _setRead(items[index], !items[index].read),
              ),
            ),
          ),
      },
    );
  }
}

class _NotificationTile extends StatelessWidget {
  const _NotificationTile({
    required this.notification,
    required this.onTap,
    required this.onToggleRead,
  });

  final AppNotification notification;
  final VoidCallback onTap;
  final VoidCallback onToggleRead;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final unread = !notification.read;

    return ListTile(
      onTap: onTap,
      onLongPress: onToggleRead,
      // Unread is carried by a dot and by weight, not by colour alone —
      // the distinction has to survive a colour-blind reader.
      leading: Icon(
        unread ? Icons.circle : Icons.circle_outlined,
        size: 12,
        color: unread ? theme.colorScheme.primary : theme.disabledColor,
      ),
      title: Text(
        notification.title,
        style: TextStyle(fontWeight: unread ? FontWeight.w600 : FontWeight.normal),
      ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(notification.body),
          const SizedBox(height: 4),
          Text(
            _relativeTime(notification.createdAt),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.textTheme.bodySmall?.color?.withValues(alpha: 0.7)),
          ),
        ],
      ),
      trailing: notification.opensText ? const Icon(Icons.chevron_right) : null,
      isThreeLine: true,
    );
  }
}

/// "il y a 3 h" and friends. Written out rather than pulled from intl: it's
/// the only place in the app that formats a date, and a whole localisation
/// dependency for one label would cost more than it saves.
String _relativeTime(DateTime moment) {
  final elapsed = DateTime.now().difference(moment);

  if (elapsed.inMinutes < 1) return "à l'instant";
  if (elapsed.inMinutes < 60) return 'il y a ${elapsed.inMinutes} min';
  if (elapsed.inHours < 24) return 'il y a ${elapsed.inHours} h';
  if (elapsed.inDays < 7) return 'il y a ${elapsed.inDays} j';

  // Past a week, "il y a 34 j" stops meaning anything — an actual date does.
  final day = moment.day.toString().padLeft(2, '0');
  final month = moment.month.toString().padLeft(2, '0');
  return '$day/$month/${moment.year}';
}
