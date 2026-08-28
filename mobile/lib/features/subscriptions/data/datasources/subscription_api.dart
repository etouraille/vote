import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/subscribed_text.dart';

class SubscriptionApi {
  SubscriptionApi._();

  /// Follows [textId]. Idempotent server-side — subscribing to a text
  /// already followed succeeds rather than erroring, so the caller never
  /// has to check first.
  static Future<void> subscribe(String textId) async {
    await ApiClient.post(Endpoints.subscribe(textId), {});
  }

  /// Stops following [textId].
  ///
  /// Ungated server-side, unlike subscribing: taking back your own
  /// attention needs no permission, and someone whose right to subscribe
  /// was revoked must still be able to leave what they had joined.
  ///
  /// Idempotent — leaving a text you no longer follow succeeds.
  static Future<void> unsubscribe(String textId) async {
    await ApiClient.delete(Endpoints.subscribe(textId), {});
  }

  /// Whether the caller may follow a text at all — root implies it,
  /// otherwise it's the canSubscribe permission. Same shape as
  /// VoteApi.canVote, and for the same reason: the button stays hidden
  /// without it rather than offering an action the api would refuse.
  static Future<bool> canSubscribe() async {
    final json = await ApiClient.get(Endpoints.me) as Map<String, dynamic>;
    if (json['root'] == true) return true;
    final permissions = json['permissions'] as Map<String, dynamic>?;
    return permissions?['canSubscribe'] == true;
  }

  /// The texts the signed-in user follows, most recently followed first.
  static Future<List<SubscribedText>> list() async {
    final json = await ApiClient.get(Endpoints.subscriptions);
    return (json as List).map((item) => SubscribedText.fromJson(item as Map<String, dynamic>)).toList();
  }
}
