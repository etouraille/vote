/// One entry of the api's notification inbox (GET /api/me/notifications).
///
/// Named AppNotification rather than Notification because Flutter already
/// has a Notification of its own in the widgets library — importing both
/// under one name is a collision waiting to happen at every call site.
class AppNotification {
  AppNotification({
    required this.id,
    required this.type,
    required this.textId,
    required this.title,
    required this.body,
    required this.createdAt,
    required this.read,
    required this.actor,
  });

  factory AppNotification.fromJson(Map<String, dynamic> json) {
    return AppNotification(
      id: json['id'] as int,
      type: json['type'] as String,
      // Absent for an event about no text in particular — the column is
      // nullable server-side and the api omits it rather than sending "".
      textId: json['textId'] as String?,
      title: json['title'] as String,
      body: json['body'] as String,
      // Parsed rather than kept as a string so the list can group and
      // format it; the api sends RFC 3339.
      createdAt: DateTime.parse(json['createdAt'] as String).toLocal(),
      read: json['read'] as bool,
      // Absent when nobody caused it — a scheduled close — or on a row
      // written before the api recorded it.
      actor: json['actor'] as String?,
    );
  }

  final int id;

  /// The event kind, e.g. `text.edit-proposed` — the same value the push
  /// payload's data map carries, so both paths can be acted on alike.
  final String type;

  /// Which text this concerns, when it concerns one. What a tap opens.
  final String? textId;

  final String title;
  final String body;
  final DateTime createdAt;

  /// Who caused it, when somebody did. The body names them too; this is
  /// for showing the name on its own rather than reading it back out of a
  /// sentence.
  final String? actor;

  /// Read state is per person, not per event: the same edit notified to
  /// five followers is five rows, each with its own read state.
  final bool read;

  /// Whether tapping this entry can lead anywhere. An entry with no text
  /// is still worth showing — it just isn't a link.
  bool get opensText => textId != null && textId!.isNotEmpty;

  AppNotification copyWith({bool? read}) {
    return AppNotification(
      id: id,
      type: type,
      textId: textId,
      title: title,
      body: body,
      createdAt: createdAt,
      read: read ?? this.read,
      actor: actor,
    );
  }
}

/// A page of the inbox together with the unread count over the *whole*
/// inbox — the api returns both in one response precisely so a list and
/// the badge beside it can never disagree.
class NotificationPage {
  NotificationPage({required this.notifications, required this.unread});

  factory NotificationPage.fromJson(Map<String, dynamic> json) {
    return NotificationPage(
      notifications: (json['notifications'] as List)
          .map((item) => AppNotification.fromJson(item as Map<String, dynamic>))
          .toList(),
      unread: json['unread'] as int,
    );
  }

  final List<AppNotification> notifications;
  final int unread;
}
