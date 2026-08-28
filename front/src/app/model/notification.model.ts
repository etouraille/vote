// The event kinds the api puts in a notification's `type` (see the api's
// notifications.go). The same values the mobile app's NotificationTypes
// carries — one vocabulary shared by every client.
export const NOTIFICATION_TYPES = {
  editProposed: 'text.edit-proposed',
  roundClosed: 'text.round-closed',
  voteCast: 'text.vote-cast',
} as const;

// One entry of GET /api/me/notifications.
export interface AppNotification {
  id: number;
  type: string;
  // Absent for an event about no text in particular — the column is
  // nullable server-side and the api omits it rather than sending "".
  textId?: string;
  title: string;
  body: string;
  createdAt: string;
  // Read state is per person, not per event: the same edit notified to
  // five followers is five rows, each with its own read state.
  read: boolean;
}

// A page of the inbox together with the unread count over the *whole*
// inbox — the api returns both in one response precisely so a list and the
// badge beside it can never disagree.
export interface NotificationPage {
  notifications: AppNotification[];
  unread: number;
}

// Where a notification leads. An edit awaiting votes opens the round;
// anything else opens the text, since there may be nothing to vote on.
//
// Unknown kinds fall to the text on purpose: it is the one destination
// valid for any text, where the vote page dead-ends on "aucun round
// ouvert" whenever the guess is wrong.
export function opensVote(type: string): boolean {
  // A vote can only be cast while a round is open, so a vote notification
  // still has a round to lead to.
  return type === NOTIFICATION_TYPES.editProposed || type === NOTIFICATION_TYPES.voteCast;
}
