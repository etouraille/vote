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

  /// The texts the signed-in user follows, most recently followed first.
  static Future<List<SubscribedText>> list() async {
    final json = await ApiClient.get(Endpoints.subscriptions);
    return (json as List).map((item) => SubscribedText.fromJson(item as Map<String, dynamic>)).toList();
  }
}
