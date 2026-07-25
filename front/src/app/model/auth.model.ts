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
