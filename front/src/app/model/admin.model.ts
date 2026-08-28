export interface Permissions {
  canVote: boolean;
  canCreateText: boolean;
  canCloseText: boolean;
  canEditText: boolean;
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
//     being the one no screen used. It, its permission and the route it
//     guarded have all since been removed.
//
// And "Créer un texte" understated itself: deleting reuses that same
// permission (see the api's deleteTextHandler), so granting it hands over
// removal too.
// Partial rather than Record, so the form can offer a subset of what an
// account holds without the type insisting on a label for every field.
// Nothing is hidden today — the one permission that was, canUpdateText, has
// since been removed outright along with the route it guarded.
export const PERMISSION_LABELS: Partial<Record<keyof Permissions, string>> = {
  canVote: 'Voter',
  canCreateText: 'Créer et supprimer un texte',
  canCloseText: 'Clore un round',
  canEditText: 'Modifier un texte',
  canSubscribe: "S'abonner à un texte",
};

export const PERMISSION_KEYS = Object.keys(PERMISSION_LABELS) as (keyof Permissions)[];
