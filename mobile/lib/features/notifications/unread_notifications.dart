import 'dart:async';

import 'package:flutter/foundation.dart';

import '../subscriptions/subscription_changes.dart';
import 'data/datasources/notification_api.dart';

/// The unread count, shared by whoever displays it.
///
/// A single ValueNotifier rather than each screen holding its own: the bell
/// appears in the app bar of every signed-in screen (see QueelAppBar), and
/// two independent copies of the count would drift apart the moment one of
/// them was the only one refreshed.
///
/// It is deliberately a plain value, not a stream of notifications: nothing
/// in the app needs the list at a distance — only the badge — and the list
/// itself is always fetched fresh when the inbox is opened.
class UnreadNotifications {
  UnreadNotifications._();

  /// Zero until the first refresh, which is also the right value for
  /// someone who isn't signed in yet — the badge simply doesn't show.
  static final ValueNotifier<int> count = ValueNotifier<int>(0);

  /// How long a freshly-read count is trusted without asking again. The
  /// bell is rebuilt on every screen, so without this, walking from the list
  /// to a text to its round would cost one request per step to answer a
  /// question whose answer cannot have changed — and when it does change,
  /// the push that changed it already bumped the count itself.
  static const _staleAfter = Duration(seconds: 30);

  static DateTime? _lastRefresh;

  static bool _watchingSubscriptions = false;

  /// Keeps the count honest when a text is left: the api drops that text's
  /// entries from the inbox, so the number in hand counts rows that no
  /// longer exist — and _staleAfter would hold that wrong number for half a
  /// minute, which is exactly long enough to be seen.
  ///
  /// Armed once, from main(), and never disarmed: the badge outlives every
  /// screen. Not from NotificationService.initialize, which returns early
  /// where push isn't configured — the inbox works there all the same.
  static void watchSubscriptions() {
    if (_watchingSubscriptions) return;
    _watchingSubscriptions = true;

    SubscriptionChanges.last.addListener(() {
      if (SubscriptionChanges.last.value?.following == false) {
        unawaited(refresh(force: true));
      }
    });
  }

  /// Re-reads the count from the api.
  ///
  /// Asks for a single-entry page: the api answers with the unread count
  /// over the whole inbox regardless of the page size, so this costs one
  /// row rather than fifty, and there is no separate count route to keep
  /// in step with the listing one.
  ///
  /// Skips the request when the count was read moments ago, unless [force]
  /// — which is what closing the inbox passes, since the point there is
  /// precisely to pick up anything that arrived while it was open.
  ///
  /// Best-effort, like everything notification-related here: a badge that
  /// failed to refresh must never surface an error over whatever the user
  /// was actually doing.
  static Future<void> refresh({bool force = false}) async {
    final last = _lastRefresh;
    if (!force && last != null && DateTime.now().difference(last) < _staleAfter) return;

    try {
      count.value = (await NotificationApi.list(limit: 1)).unread;
      _lastRefresh = DateTime.now();
    } catch (error) {
      debugPrint('notifications: rafraîchissement du compteur impossible: $error');
    }
  }

  /// Records the arrival of a push while the app is open, without a round
  /// trip: the inbox row was written by the same fan-out that sent the
  /// push, so the count is one higher whether or not we ask.
  static void increment() => count.value++;

  /// Sets the count outright — used by the inbox screen, which already
  /// knows the exact figure and shouldn't make the server say it twice.
  static void set(int value) => count.value = value < 0 ? 0 : value;

  /// Signing out must clear it, or the next person to sign in on this
  /// device inherits a badge counting someone else's notifications.
  ///
  /// Also drops the freshness stamp, so the next bell to appear asks
  /// again rather than trusting a count read as somebody else.
  static void clear() {
    count.value = 0;
    _lastRefresh = null;
  }
}
