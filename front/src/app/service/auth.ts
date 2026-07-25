import { HttpClient } from '@angular/common/http';
import { Service, computed, inject, signal } from '@angular/core';
import { Observable } from 'rxjs';
import { map, tap } from 'rxjs/operators';
import { API_BASE_URL } from '../api-base-url';
import { AuthResponse, AuthTokens, LoginCredentials, RegisterAck, RegisterCredentials, User } from '../model/auth.model';
import { MeResponse } from '../model/admin.model';
import { TokenStorage } from './token-storage';

@Service()
export class AuthService {
  private readonly tokenStorage = inject(TokenStorage);
  private readonly http = inject(HttpClient);

  private readonly _tokens = signal<AuthTokens | null>(this.tokenStorage.read());
  private readonly _user = signal<User | null>(null);

  readonly user = this._user.asReadonly();
  readonly accessToken = computed(() => this._tokens()?.accessToken ?? null);
  readonly isAuthenticated = computed(() => {
    const tokens = this._tokens();
    return !!tokens && tokens.expiresAt > Date.now();
  });

  constructor() {
    // A reload keeps the token (restored above from storage) but not the
    // in-memory `user` signal — re-populate it from /api/me so the pseudo
    // / avatar bar survives a refresh instead of only appearing right after
    // a fresh login.
    //
    // Deferred to a microtask: authInterceptor injects AuthService on every
    // HttpClient request, so calling http.get(...) synchronously in this
    // constructor — before Angular's DI has finished constructing this very
    // instance — is a circular dependency (NG0200). Queuing it lets
    // construction finish first.
    if (this.isAuthenticated()) {
      queueMicrotask(() => {
        this.me().subscribe((me) => {
          this._user.set({
            id: me.userId,
            email: me.email,
            pseudo: me.pseudo,
            displayName: me.pseudo || me.email.split('@')[0],
            root: me.root,
          });
        });
      });
    }
  }

  login(credentials: LoginCredentials): Observable<User> {
    return this.http.post<AuthResponse>(`${API_BASE_URL}/api/auth/login`, credentials).pipe(
      map((response) => this.toSession(response)),
      tap(({ user, tokens }) => {
        this.tokenStorage.write(tokens);
        this._tokens.set(tokens);
        this._user.set(user);
      }),
      map(({ user }) => user),
    );
  }

  register(credentials: RegisterCredentials): Observable<RegisterAck> {
    return this.http.post<RegisterAck>(`${API_BASE_URL}/api/auth/register`, credentials);
  }

  // Who the caller is and what they're allowed to do, straight from their
  // JWT claims (see api's meHandler) — used by adminGuard to decide
  // whether the backoffice is reachable. Not cached: rights can change
  // between visits, and this is only called when it actually matters.
  me(): Observable<MeResponse> {
    return this.http.get<MeResponse>(`${API_BASE_URL}/api/me`);
  }

  confirm(email: string, code: string): Observable<{ message: string }> {
    return this.http.post<{ message: string }>(`${API_BASE_URL}/api/auth/confirm`, { email, code: Number(code) });
  }

  logout(): void {
    this.tokenStorage.clear();
    this._tokens.set(null);
    this._user.set(null);
  }

  private toSession(response: AuthResponse): { user: User; tokens: AuthTokens } {
    return {
      user: {
        id: response.userId,
        email: response.email,
        pseudo: response.pseudo,
        displayName: response.pseudo || response.email.split('@')[0],
        root: response.root,
      },
      tokens: { accessToken: response.token, expiresAt: Date.parse(response.expiresAt) },
    };
  }
}
