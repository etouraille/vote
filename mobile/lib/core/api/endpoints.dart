/// api route paths, relative to Env.apiBaseUrl (see ApiClient) — kept here
/// so a path never needs retyping (or drifting) at each call site.
class Endpoints {
  Endpoints._();

  static const login = '/api/auth/login';

  /// "Sign in with Google" — takes a Google ID token instead of a password.
  /// Only served when api's own GOOGLE_CLIENT_ID is set (see main.go), so a
  /// backend without it configured answers 404 here.
  static const googleLogin = '/api/auth/google';


  static String text(String id) => '/api/texts/$id';

  /// The most recent articles, newest first, each with its tags and
  /// whether the caller follows it. Superseded versions are left out by
  /// the api, so this lists every article at its latest round.
  ///
  /// [tags] narrows it: an article has to carry *every* one given, so each
  /// label added leaves fewer. Repeated as its own parameter rather than
  /// packed into a list, which would split a label containing a comma.
  static String recentTexts(int limit, int offset, {List<String> tags = const []}) {
    final query = [
      'limit=$limit',
      'offset=$offset',
      for (final tag in tags) 'tag=${Uri.encodeQueryComponent(tag)}',
    ];
    return '/api/texts?${query.join('&')}';
  }

  /// The labels in use, most used first — what the filter offers.
  static const tags = '/api/tags';

  /// Follow a text, or stop — the same path, POST to join and DELETE to
  /// leave. The caller is taken from the bearer token, so nothing
  /// identifies the user in the path or body.
  static String subscribe(String id) => '/api/texts/$id/subscribe';

  /// The texts the caller follows, id and title only.
  static const subscriptions = '/api/me/subscriptions';

  /// Who the caller is and what they may do — the vote page reads
  /// permissions.canVote from it.
  static const me = '/api/me';

  /// Registers this device's push token against the signed-in user.
  static const devices = '/api/me/devices';

  /// The caller's notification inbox, newest first, with the unread count
  /// alongside it. The same events push delivers (see NotificationService),
  /// kept server-side so the history survives a reinstall and reads the
  /// same on every device.
  static String notifications({List<String> types = const [], int? limit}) {
    final query = <String>[
      if (types.isNotEmpty) 'types=${types.join(',')}',
      if (limit != null) 'limit=$limit',
    ];
    return '/api/me/notifications${query.isEmpty ? '' : '?${query.join('&')}'}';
  }

  /// Flips one notification between read and unread — the body carries
  /// which, so the same route serves both directions.
  static String notificationRead(int id) => '/api/me/notifications/$id/read';

  /// Empties the badge in one call.
  static const notificationsReadAll = '/api/me/notifications/read-all';

  /// A text plus the slots of its current round, if any. No open round is a
  /// 200 with roundNumber 0 and no slots, not a 404.
  static String textWithSlots(String id) => '/api/texts/$id/with-slots';

  /// The competing proposals for one slot, including the seed fragment that
  /// stands for "keep the original wording".
  static String slotFragments(String textId, String slotId) => '/api/texts/$textId/slots/$slotId/fragments';

  /// Which fragment the caller currently has voted for in each slot of a
  /// text, keyed by slot id. Slots they haven't voted in are absent. The
  /// caller comes from the bearer token, so nothing names them in the path.
  static String myVotes(String textId) => '/api/texts/$textId/my-votes';

  static String castVote(String fragmentId) => '/api/fragments/$fragmentId/vote';
}
