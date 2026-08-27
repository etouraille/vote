export interface CreateTextResponse {
  id: string;
}

export interface Text {
  id: string;
  title: string;
  content: string;
  finalized: boolean;
  createdAt: string;
}

// What GET /api/texts (the home page's "Derniers textes") returns — a Text
// plus whether the current user follows it, same gating role as
// SearchResult.subscribed below.
export interface RecentText extends Text {
  subscribed: boolean;
  // Which round is currently open on this text — the same count
  // SearchResult carries, so both listings can label where a text stands.
  roundNumber: number;
}

// One entry of GET /api/me/subscriptions — a followed text reduced to what
// a list of titles needs. Narrower than Text server-side too: carrying every
// followed text's whole content just to name it would bloat the response.
export interface SubscribedText {
  id: string;
  title: string;
}

export interface SearchResult {
  textId: string;
  title: string;
  score: number;
  // 0 means no round is currently open on this text.
  roundNumber: number;
  // Whether the current user follows this text — gates whether the
  // vote/edit/close/delete actions are shown for it (see home.ts).
  subscribed: boolean;
}

// One slot of a past round: the wording it replaced, the wording that won,
// and by how many votes. Original is sliced out of the version the round
// ran on — the only place that wording still exists once the fork spliced
// the winner in.
export interface HistorySlot {
  slotId: string;
  original: string;
  winner: string;
  votes: number;
  authorId?: string;
}

export interface HistoryRound {
  number: number;
  status: 'open' | 'closed';
  slots: HistorySlot[];
}

// One link of a text's chain of versions, with the rounds that ran on it.
export interface HistoryVersion {
  textId: string;
  title: string;
  content: string;
  createdAt: string;
  finalized: boolean;
  rounds: HistoryRound[];
}

export interface Fragment {
  id: string;
  textId: string;
  slotId: string;
  content: string;
  authorId: string;
  createdAt: string;
}

// What POST /api/texts/{id}/close-round answers with: the round that just
// closed, the brand new version it produced, and how each slot was settled.
//
// text is a *different* text from the one closed — closing forks rather
// than mutates (see queel's CloseRound), so its id is new and it is the one
// anything further should point at.
export interface RoundOutcome {
  // The round that just closed. The version it produced carries the next
  // one, already open (see queel's CloseRound).
  round: { number: number };
  text: Text;
  slots: SlotResult[];
}

export interface SlotResult {
  slotId: string;
  fragment: Fragment | null;
  votes: number;
}

export interface Slot {
  id: string;
  start: number;
  end: number;
  round: number;
}

export interface TextWithSlots {
  text: Text;
  roundNumber: number;
  slots: Slot[];
}
