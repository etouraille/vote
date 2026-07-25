export interface User {
  id: string;
  email: string;
  displayName: string;
  pseudo?: string;
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
}

export interface RegisterAck {
  email: string;
  message: string;
}
