import { Component, effect, inject, signal } from '@angular/core';
import { Router, RouterLink, RouterOutlet } from '@angular/router';
import { AuthService } from './service/auth';
import { NotificationService } from './service/notification';
import { PushService } from './service/push';
import { opensVote } from './model/notification.model';
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
  private readonly push = inject(PushService);
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
      if (this.auth.isAuthenticated()) {
        this.notifications.refreshUnread();
        // After sign-in, never at startup: the api takes this browser's
        // owner from the bearer token. A no-op until Firebase is
        // configured, and never awaited — the page must not wait on a
        // permission prompt.
        void this.push.register();
      } else {
        this.notifications.clear();
      }
    });

    // A notification clicked while the tab was in the background: the
    // service worker cannot route, so it hands the event here (see
    // public/firebase-messaging-sw.js).
    if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator) {
      navigator.serviceWorker.addEventListener('message', (event) => {
        if (event.data?.type !== 'queel:notification-click') return;
        this.openNotification(event.data.data ?? {});
      });
    }
  }

  // Routes a clicked push the same way the inbox does, from the one place
  // that decides it (see opensVote).
  private openNotification(data: Record<string, string>): void {
    if (!data['textId']) {
      this.router.navigateByUrl('/notifications');
      return;
    }
    this.router.navigate([opensVote(data['type'] ?? '') ? '/vote' : '/text'], {
      queryParams: { id: data['textId'] },
    });
  }

  logout(): void {
    // Before the session is cleared: the api scopes the device deletion to
    // the caller, so it has nothing to match on once the token is gone.
    // Not awaited — signing out must never wait on it.
    void this.push.unregister();

    this.auth.logout();
    this.notifications.clear();
    this.router.navigateByUrl('/');
  }
}
