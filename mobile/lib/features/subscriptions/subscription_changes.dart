import 'package:flutter/foundation.dart';

/// Who follows what, as it changes, for the screens that show it without
/// owning it.
///
/// The article list paints a green mark on every text the reader follows,
/// and the subscriptions screen is a list of exactly those texts — but
/// leaving a text happens on a third screen as often as not. Without this,
/// each list only learns of a change by being rebuilt from the api, which
/// is why unfollowing from "Mes abonnements" left the mark standing on the
/// article list underneath.
///
/// A single value rather than a stream: nothing needs the history, only
/// the latest change, and a screen that missed one was not on screen to
/// show it. Same shape as [UnreadNotifications], for the same reason — one
/// source of truth for something several screens display.
class SubscriptionChanges {
  SubscriptionChanges._();

  /// The last change: a text, and whether the reader now follows it. Null
  /// until something changes in this session.
  static final ValueNotifier<({String textId, bool following})?> last = ValueNotifier(null);

  static void record(String textId, {required bool following}) =>
      last.value = (textId: textId, following: following);
}
