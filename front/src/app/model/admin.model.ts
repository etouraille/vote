export interface Permissions {
  canVote: boolean;
  canCreateText: boolean;
  canCloseText: boolean;
  canSelect: boolean;
  canEditSelection: boolean;
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

export const PERMISSION_LABELS: Record<keyof Permissions, string> = {
  canVote: 'Voter',
  canCreateText: 'Créer un texte',
  canCloseText: 'Clore un round',
  canSelect: 'Sélectionner une plage',
  canEditSelection: 'Proposer un contenu',
  canUpdateText: 'Modifier un texte directement',
  canSubscribe: "S'abonner à un texte",
};

export const PERMISSION_KEYS = Object.keys(PERMISSION_LABELS) as (keyof Permissions)[];
