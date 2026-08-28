/// The event kinds the api puts in a notification's `type` — the push
/// payload's data map and the inbox row carry the same value, so both can
/// be acted on alike (see the api's notifications.go).
///
/// Kept here rather than beside either reader because a tapped notification
/// reaches the app by two unrelated routes — Firebase for one drawn by
/// Android, the local-notifications plugin for one drawn by the app itself
/// — plus a third from the in-app inbox. All three have to agree on where a
/// given kind leads, and they only do so as long as they read one
/// definition.
class NotificationTypes {
  NotificationTypes._();

  /// Somebody carved out a slot and proposed a wording for it. There is a
  /// round open, so the useful destination is the vote page.
  static const editProposed = 'text.edit-proposed';

  /// A round was closed and the text forked into its next version. The id
  /// carried is the fork's, and no round is open on it — so this leads to
  /// the text, never to the vote page.
  static const roundClosed = 'text.round-closed';

  /// Somebody voted on a fragment of a text you follow. A vote can only be
  /// cast while a round is open, so the round is still there to go to.
  static const voteCast = 'text.vote-cast';

  /// Whether a notification of this kind should open the vote page rather
  /// than the reading page.
  ///
  /// Unknown kinds fall to the reading page on purpose: it is the one
  /// destination that is valid for any text, where the vote page dead-ends
  /// on "aucun round ouvert" whenever the guess is wrong.
  static bool opensVote(String? type) => type == editProposed || type == voteCast;
}
