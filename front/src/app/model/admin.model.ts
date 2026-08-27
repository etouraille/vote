export interface Permissions {
  canVote: boolean;
  canCreateText: boolean;
  canCloseText: boolean;
  canEditText: boolean;
  canUpdateText: boolean;
  // Following a text. Not a right over the text itself, but it gates what
  // the app offers: only followed texts show their vote/edit/close actions,
  // and only followers are notified when one changes.
  canSubscribe: boolean;
}

export interface AdminUser {
  id: string;
  email: string;
  root: boolean;
  permissions: Permissions;
}

export interface MeResponse {
  userId: string;
  email: string;
  pseudo?: string;
  root: boolean;
  permissions: Permissions;
}

// What each checkbox is called in the backoffice.
//
// These have to describe what the api actually gates, not what the field is
// named — an administrator ticks a box and expects a specific button to
// appear for someone. Three of them used to mislead:
//
//   - "Sélectionner une plage" and "Proposer un contenu" were two boxes for
//     one act — they have since become the single canEditText.
//   - "Modifier un texte directement" read as *the* editing right while
//     being the one no screen uses: it gates PUT /api/texts/{id}, the back
//     door that overwrites a text without a vote.
//
// And "Créer un texte" understated itself: deleting reuses that same
// permission (see the api's deleteTextHandler), so granting it hands over
// removal too.
export const PERMISSION_LABELS: Record<keyof Permissions, string> = {
  canVote: 'Voter',
  canCreateText: 'Créer et supprimer un texte',
  canCloseText: 'Clore un round',
  canEditText: 'Modifier un texte',
  canUpdateText: 'Écraser le texte sans vote',
  canSubscribe: "S'abonner à un texte",
};

export const PERMISSION_KEYS = Object.keys(PERMISSION_LABELS) as (keyof Permissions)[];
