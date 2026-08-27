import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/app_notification.dart';

/// The read side of notifications: what push already delivered, fetched
/// back from the api's inbox so it can be reviewed after the fact.
class NotificationApi {
  NotificationApi._();

  /// The caller's inbox, newest first, plus the unread count.
  ///
  /// [limit] is capped server-side; leaving it unset takes the api's own
  /// default rather than duplicating that number here.
  static Future<NotificationPage> list({int? limit}) async {
    final path = limit == null ? Endpoints.notifications : '${Endpoints.notifications}?limit=$limit';
    return NotificationPage.fromJson(await ApiClient.get(path) as Map<String, dynamic>);
  }

  /// Marks one notification read, or unread again — both directions
  /// through the same route, so an inbox stays revisitable.
  static Future<void> setRead(int id, bool read) async {
    await ApiClient.put(Endpoints.notificationRead(id), {'read': read});
  }

  /// Acknowledges the whole inbox at once. Returns how many rows actually
  /// changed, which is the unread count that just went away.
  static Future<int> markAllRead() async {
    final json = await ApiClient.post(Endpoints.notificationsReadAll, {});
    return (json as Map<String, dynamic>)['updated'] as int;
  }
}
