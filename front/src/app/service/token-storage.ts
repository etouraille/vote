import { Service } from '@angular/core';
import { AuthTokens } from '../model/auth.model';

const STORAGE_KEY = 'vote.auth.tokens';

@Service()
export class TokenStorage {
  read(): AuthTokens | null {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    try {
      return JSON.parse(raw) as AuthTokens;
    } catch {
      return null;
    }
  }

  write(tokens: AuthTokens): void {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(tokens));
  }

  clear(): void {
    localStorage.removeItem(STORAGE_KEY);
  }
}
