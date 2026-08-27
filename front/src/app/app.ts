import { Component, effect, inject, signal } from '@angular/core';
import { Router, RouterLink, RouterOutlet } from '@angular/router';
import { AuthService } from './service/auth';
import { NotificationService } from './service/notification';
import { gravatarUrl } from './util/gravatar';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink],
  templateUrl: './app.html',
  styleUrl: './app.css',
})
export class App {
  protected readonly title = signal('front');

  protected readonly auth = inject(AuthService);
  protected readonly notifications = inject(NotificationService);
  private readonly router = inject(Router);
  protected readonly avatarUrl = signal<string | null>(null);

  constructor() {
    // Gravatar's hash is derived async (native SubtleCrypto), so it can't
    // be a plain computed() — re-run whenever the logged-in user (and thus
    // their email) changes, including from null to a value on login.
    effect(() => {
      const email = this.auth.user()?.email;
      if (!email) {
        this.avatarUrl.set(null);
        return;
      }
      gravatarUrl(email).then((url) => this.avatarUrl.set(url));
    });

    // The badge follows the session: filled on sign-in (and on a reload
    // that restored one), emptied on sign-out so the next person in this
    // tab doesn't inherit someone else's count.
    effect(() => {
      if (this.auth.isAuthenticated()) this.notifications.refreshUnread();
      else this.notifications.clear();
    });
  }

  logout(): void {
    this.auth.logout();
    this.notifications.clear();
    this.router.navigateByUrl('/');
  }
}
