import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/vote_models.dart';

class VoteApi {
  VoteApi._();

  static Future<TextWithSlots> textWithSlots(String textId) async {
    final json = await ApiClient.get(Endpoints.textWithSlots(textId));
    return TextWithSlots.fromJson(json as Map<String, dynamic>);
  }

  static Future<List<Fragment>> fragmentsForSlot(String textId, String slotId) async {
    final json = await ApiClient.get(Endpoints.slotFragments(textId, slotId));
    return (json as List).map((item) => Fragment.fromJson(item as Map<String, dynamic>)).toList();
  }

  /// Which fragment the caller already voted for in each slot, keyed by
  /// slot id. Slots they haven't voted in are absent.
  ///
  /// Without this the page could only show a vote cast during the current
  /// visit: the choice has always been recorded server-side, it simply had
  /// no way back to the client.
  static Future<Map<String, String>> myVotes(String textId) async {
    final json = await ApiClient.get(Endpoints.myVotes(textId)) as Map<String, dynamic>;
    return json.map((slotId, fragmentId) => MapEntry(slotId, fragmentId as String));
  }

  /// Casts the caller's vote for [fragmentId]. A slot holds at most one
  /// vote per user — voting for a different fragment of the same slot
  /// withdraws the previous one rather than adding to it.
  static Future<void> castVote(String fragmentId) async {
    await ApiClient.post(Endpoints.castVote(fragmentId), {});
  }

  /// Whether the caller may vote at all — root implies it, otherwise it's
  /// the canVote permission. The vote controls stay hidden without it,
  /// rather than offering an action the api would refuse.
  static Future<bool> canVote() async {
    final json = await ApiClient.get(Endpoints.me) as Map<String, dynamic>;
    if (json['root'] == true) return true;
    final permissions = json['permissions'] as Map<String, dynamic>?;
    return permissions?['canVote'] == true;
  }
}
