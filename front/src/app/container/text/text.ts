import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { Text } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';

// Reading view of a single text, opened by clicking a search result's title
// on the home page.
//
// Following the text is the one action it offers. It belongs here because
// this is where someone decides a text is worth their attention — and
// because everything else (editing, voting, closing) only appears on a text
// once it is followed, so without this the page would be a dead end for any
// text reached from a search result rather than from the cards.
@Component({
  selector: 'text-page',
  imports: [RouterLink],
  templateUrl: './text.html',
})
export class TextPage implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly textService = inject(TextService);
  private readonly auth = inject(AuthService);

  readonly text = signal<Text | null>(null);
  readonly loading = signal(true);
  readonly loadError = signal<string | null>(null);

  // null until the answer is known: the button stays hidden rather than
  // claiming "not subscribed" while the list is still in flight, which
  // would flicker into "Abonné" for anyone who already follows the text.
  readonly subscribed = signal<boolean | null>(null);

  // Following a text is a permission of its own (see api's
  // subscribeHandler). Without it the button is hidden rather than shown
  // and rejected.
  readonly canSubscribe = signal(false);
  readonly subscribing = signal(false);
  readonly subscribeError = signal<string | null>(null);

  private textId: string | null = null;

  ngOnInit(): void {
    const id = this.route.snapshot.queryParamMap.get('id');
    if (!id) {
      this.loading.set(false);
      this.loadError.set("Aucun id de texte fourni dans l'url (?id=...).");
      return;
    }
    this.textId = id;

    this.auth.me().subscribe((me) => this.canSubscribe.set(me.root || me.permissions.canSubscribe));

    this.textService.get(id).subscribe({
      next: (text) => {
        this.text.set(text);
        this.loading.set(false);
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.loadError.set(err.status === 404 ? "Ce texte n'existe pas ou plus." : 'Chargement impossible.');
      },
    });

    // Separate from the text load rather than a forkJoin: reading the text
    // is the point of the page, and it must not fail because the follow
    // state couldn't be resolved. A failure here simply leaves the button
    // hidden.
    //
    // GET /api/texts/{id} doesn't say whether the caller follows the text —
    // only the list and search responses carry that flag — so the answer
    // comes from the caller's own subscription list instead. It's their
    // personal follow list, so it stays small; adding the field to the text
    // route would mean changing its shape and mirroring that in
    // queel/server for a boolean this page alone needs.
    this.textService.subscriptions().subscribe({
      next: (subscriptions) => this.subscribed.set(subscriptions.some((item) => item.id === id)),
      error: () => this.subscribed.set(null),
    });
  }

  subscribe(): void {
    const id = this.textId;
    if (!id || this.subscribing()) return;

    this.subscribing.set(true);
    this.subscribeError.set(null);

    this.textService.subscribe(id).subscribe({
      next: () => {
        this.subscribing.set(false);
        this.subscribed.set(true);
      },
      error: (err: HttpErrorResponse) => {
        this.subscribing.set(false);
        this.subscribeError.set(err.error?.error ?? "Abonnement impossible.");
      },
    });
  }
}
