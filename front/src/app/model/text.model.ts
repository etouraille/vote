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

export interface Fragment {
  id: string;
  textId: string;
  slotId: string;
  content: string;
  authorId: string;
  createdAt: string;
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
