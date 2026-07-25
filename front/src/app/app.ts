import { Component, effect, inject, signal } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { AuthService } from './service/auth';
import { gravatarUrl } from './util/gravatar';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.css'
})
export class App {
  protected readonly title = signal('front');

  protected readonly auth = inject(AuthService);
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
  }
}
