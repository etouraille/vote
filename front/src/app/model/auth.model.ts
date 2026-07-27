import { Permissions } from './admin.model';

export interface User {
  id: string;
  email: string;
  displayName: string;
  pseudo?: string;
  root: boolean;
  canCreateText: boolean;
}

export interface AuthTokens {
  accessToken: string;
  refreshToken?: string;
  expiresAt: number;
}

export interface LoginCredentials {
  email: string;
  password: string;
}

export interface RegisterCredentials extends LoginCredentials {
  pseudo?: string;
}

export interface AuthResponse {
  token: string;
  expiresAt: string;
  userId: string;
  email: string;
  pseudo?: string;
  root: boolean;
  permissions: Permissions;
}

export interface RegisterAck {
  email: string;
  message: string;
}

export interface GoogleAuthRequest {
  idToken: string;
  // Required only the first time a given Google account signs in — see
  // AuthService.googleLogin.
  pseudo?: string;
}

// api's POST /api/auth/google returns this instead of an AuthResponse when
// no account exists yet for the signed-in Google email: Google's identity
// doesn't supply a pseudo, and it's mandatory, so account creation is held
// off until the caller retries with one (see AuthService.googleLogin).
export interface GoogleAuthNeedsPseudo {
  needsPseudo: true;
}

export type GoogleAuthResult = GoogleAuthNeedsPseudo | AuthResponse;
