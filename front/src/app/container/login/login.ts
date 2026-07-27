import { HttpErrorResponse } from '@angular/common/http';
import { AfterViewInit, Component, ElementRef, NgZone, ViewChild, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { GOOGLE_CLIENT_ID } from '../../google-client-id';
import { AuthService } from '../../service/auth';

// Minimal shape of the pieces of Google Identity Services (see
// index.html's script tag) this component actually calls — not a full
// typing of Google's SDK, just enough to avoid `any` at every call site.
interface GoogleIdentityServices {
  accounts: {
    id: {
      initialize(config: { client_id: string; callback: (response: { credential: string }) => void }): void;
      renderButton(parent: HTMLElement, options: { theme: string; size: string; text: string; width?: number }): void;
    };
  };
}

declare global {
  interface Window {
    google?: GoogleIdentityServices;
  }
}

@Component({
  selector: 'login-page',
  imports: [FormsModule, RouterLink],
  templateUrl: './login.html',
})
export class LoginPage implements AfterViewInit {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly zone = inject(NgZone);

  @ViewChild('googleButton') private googleButtonRef?: ElementRef<HTMLDivElement>;

  email = '';
  password = '';
  pseudo = '';
  readonly submitting = signal(false);
  readonly error = signal<string | null>(null);

  // Set once Google hands back a credential — needed again if a second
  // request (with the now-collected pseudo) turns out to be necessary, see
  // needsPseudoForGoogleSignIn.
  private pendingGoogleIdToken = '';
  readonly needsPseudoForGoogleSignIn = signal(false);

  constructor() {
    if (this.auth.isAuthenticated()) {
      this.router.navigateByUrl('/home');
    }
  }

  ngAfterViewInit(): void {
    this.renderGoogleButton();
  }

  private renderGoogleButton(): void {
    const google = window.google;
    if (!google || !this.googleButtonRef) return;

    google.accounts.id.initialize({
      client_id: GOOGLE_CLIENT_ID,
      // Google calls this outside Angular's zone — without zone.run, the
      // signals/navigation below would update but the view wouldn't.
      callback: (response) => this.zone.run(() => this.onGoogleCredential(response.credential)),
    });
    google.accounts.id.renderButton(this.googleButtonRef.nativeElement, {
      theme: 'outline',
      size: 'large',
      text: 'continue_with',
      width: 320,
    });
  }

  private onGoogleCredential(idToken: string): void {
    this.pendingGoogleIdToken = idToken;
    this.pseudo = '';
    this.error.set(null);
    this.continueGoogleSignIn();
  }

  continueGoogleSignIn(): void {
    if (this.submitting()) return;
    this.submitting.set(true);
    this.error.set(null);

    this.auth.googleLogin(this.pendingGoogleIdToken, this.pseudo || undefined).subscribe({
      next: (result) => {
        this.submitting.set(false);
        if ('needsPseudo' in result) {
          this.needsPseudoForGoogleSignIn.set(true);
          return;
        }
        this.needsPseudoForGoogleSignIn.set(false);
        this.router.navigateByUrl('/home');
      },
      error: (err: HttpErrorResponse) => {
        this.submitting.set(false);
        this.error.set(err.error?.error ?? 'Connexion Google impossible.');
      },
    });
  }

  submit(): void {
    if (this.submitting()) return;
    this.submitting.set(true);
    this.error.set(null);
    this.auth.login({ email: this.email, password: this.password }).subscribe({
      next: () => {
        this.submitting.set(false);
        this.router.navigateByUrl('/home');
      },
      error: (err: HttpErrorResponse) => {
        this.submitting.set(false);
        this.error.set(err.error?.error ?? 'Email ou mot de passe invalide');
      },
    });
  }
}
