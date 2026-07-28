import { HttpErrorResponse } from '@angular/common/http';
import { AfterViewInit, Component, ElementRef, NgZone, OnDestroy, ViewChild, inject, signal } from '@angular/core';
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

const GOOGLE_SCRIPT_POLL_MS = 100;
const GOOGLE_SCRIPT_TIMEOUT_MS = 10_000;

@Component({
  selector: 'login-page',
  imports: [FormsModule, RouterLink],
  templateUrl: './login.html',
})
export class LoginPage implements AfterViewInit, OnDestroy {
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

  // True once we've waited out GOOGLE_SCRIPT_TIMEOUT_MS without Google
  // Identity Services ever showing up, so the page can say so instead of
  // leaving an empty gap where the button should be.
  readonly googleUnavailable = signal(false);
  private googleReadyTimer?: ReturnType<typeof setInterval>;

  constructor() {
    if (this.auth.isAuthenticated()) {
      this.router.navigateByUrl('/home');
    }
  }

  ngAfterViewInit(): void {
    this.whenGoogleIdentityServicesReady(() => this.renderGoogleButton());
  }

  ngOnDestroy(): void {
    this.clearGoogleReadyTimer();
  }

  // index.html loads Google Identity Services with `async defer`, so
  // window.google is regularly still undefined by the time this view
  // initializes — rendering straight away dropped the button silently
  // whenever the script lost that race against Angular's bootstrap.
  private whenGoogleIdentityServicesReady(render: () => void): void {
    if (window.google) {
      render();
      return;
    }

    // Outside the zone: this polls every 100ms and would otherwise kick off
    // a change detection run each tick for the whole wait.
    this.zone.runOutsideAngular(() => {
      let waited = 0;
      this.googleReadyTimer = setInterval(() => {
        waited += GOOGLE_SCRIPT_POLL_MS;
        if (window.google) {
          this.clearGoogleReadyTimer();
          this.zone.run(render);
        } else if (waited >= GOOGLE_SCRIPT_TIMEOUT_MS) {
          this.clearGoogleReadyTimer();
          this.zone.run(() => this.googleUnavailable.set(true));
        }
      }, GOOGLE_SCRIPT_POLL_MS);
    });
  }

  private clearGoogleReadyTimer(): void {
    if (this.googleReadyTimer !== undefined) {
      clearInterval(this.googleReadyTimer);
      this.googleReadyTimer = undefined;
    }
  }

  private renderGoogleButton(): void {
    const google = window.google;
    if (!google || !this.googleButtonRef) {
      this.googleUnavailable.set(true);
      return;
    }
    this.googleUnavailable.set(false);

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
