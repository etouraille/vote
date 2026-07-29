/// api route paths, relative to Env.apiBaseUrl (see ApiClient) — kept here
/// so a path never needs retyping (or drifting) at each call site.
class Endpoints {
  Endpoints._();

  static const login = '/api/auth/login';

  /// "Sign in with Google" — takes a Google ID token instead of a password.
  /// Only served when api's own GOOGLE_CLIENT_ID is set (see main.go), so a
  /// backend without it configured answers 404 here.
  static const googleLogin = '/api/auth/google';

  static String search(String query) => '/api/texts/search?q=${Uri.encodeQueryComponent(query)}';

  static String text(String id) => '/api/texts/$id';

  /// Follow a text — the caller is taken from the bearer token, so nothing
  /// identifies the user in the path or body.
  static String subscribe(String id) => '/api/texts/$id/subscribe';

  /// The texts the caller follows, id and title only.
  static const subscriptions = '/api/me/subscriptions';

  /// Who the caller is and what they may do — the vote page reads
  /// permissions.canVote from it.
  static const me = '/api/me';

  /// Registers this device's push token against the signed-in user.
  static const devices = '/api/me/devices';

  /// A text plus the slots of its current round, if any. No open round is a
  /// 200 with roundNumber 0 and no slots, not a 404.
  static String textWithSlots(String id) => '/api/texts/$id/with-slots';

  /// The competing proposals for one slot, including the seed fragment that
  /// stands for "keep the original wording".
  static String slotFragments(String textId, String slotId) => '/api/texts/$textId/slots/$slotId/fragments';

  static String castVote(String fragmentId) => '/api/fragments/$fragmentId/vote';
}
