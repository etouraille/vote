import { DatePipe, DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { SearchResult } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';
import { firstWords } from '../../util/words';

// Was 4 (just enough for 2 rows of the 2-column grid) back when the
// section couldn't scroll — now that it's a fixed-height scrollable zone
// (see home.html), fetch a proper batch to actually scroll through,
// matching the API's own default limit.
const RECENT_TEXTS_COUNT = 20;
const EXCERPT_WORD_COUNT = 100;

export interface RecentTextCard {
  textId: string;
  title: string;
  excerpt: string;
  subscribed: boolean;
}

// The shape every subscribe/close-round/delete action needs — satisfied by
// both SearchResult and RecentTextCard, so the same handful of methods
// drive the action buttons on both the search-results list and the
// "Derniers textes" grid instead of duplicating them per list.
interface ActionTarget {
  textId: string;
  title: string;
}

@Component({
  selector: 'home-page',
  imports: [FormsModule, DecimalPipe, DatePipe, RouterLink],
  templateUrl: './home.html',
})
export class HomePage implements OnInit {
  private readonly auth = inject(AuthService);
  private readonly textService = inject(TextService);

  searchQuery = '';

  readonly searching = signal(false);
  readonly searchResults = signal<SearchResult[] | null>(null);
  readonly searchError = signal<string | null>(null);

  readonly recentTexts = signal<RecentTextCard[]>([]);

  // Gate the Éditer/Voter/Clore un round actions shown on each search
  // result — same rights that gate the actions themselves once you're on
  // those pages (editor.ts's canAmendText, vote.ts's canVote), so a result
  // never links to a page (or offers an action) the user couldn't actually
  // use anyway. On top of the permission, each of these also requires
  // result.subscribed (see subscribe() below) — a text only surfaces its
  // action buttons once the user has chosen to follow it.
  readonly canAmendText = signal(false);
  readonly canVote = signal(false);
  readonly canCloseText = signal(false);

  // Deleting a text outright reuses the canCreateText permission as its
  // bar (see api's deleteTextHandler) — whoever can create texts can also
  // remove one from here, rather than a dedicated permission bit.
  readonly canDeleteText = signal(false);

  // Per-result subscribe state, same shape as close-round/delete above.
  readonly subscribingTextId = signal<string | null>(null);
  readonly subscribeErrors = signal<Readonly<Record<string, string>>>({});

  // Per-result close-round state, keyed by textId — a search list can hold
  // several results at once, each closeable independently.
  readonly closingTextId = signal<string | null>(null);
  readonly closedTextIds = signal<ReadonlySet<string>>(new Set());
  readonly closeErrors = signal<Readonly<Record<string, string>>>({});

  // The "Clore un round" popin: which result it's open for (null = closed),
  // and the day count bound to its "dans N jours" input.
  readonly closeRoundPopup = signal<ActionTarget | null>(null);
  closeDays = 7;

  // Set once scheduleClose succeeds, so the row can show when the round is
  // due to close on its own instead of (or alongside) the manual button.
  readonly scheduledCloseAt = signal<Readonly<Record<string, string>>>({});

  // Per-result delete state, same shape as close-round above.
  readonly deletingTextId = signal<string | null>(null);
  readonly deleteErrors = signal<Readonly<Record<string, string>>>({});

  ngOnInit(): void {
    this.auth.me().subscribe((me) => {
      this.canAmendText.set(me.root || me.permissions.canSelect || me.permissions.canEditSelection);
      this.canVote.set(me.root || me.permissions.canVote);
      this.canCloseText.set(me.root || me.permissions.canCloseText);
      this.canDeleteText.set(me.root || me.permissions.canCreateText);
    });
    this.loadRecentTexts();
  }

  private loadRecentTexts(): void {
    this.textService.listRecent(RECENT_TEXTS_COUNT).subscribe((texts) => {
      this.recentTexts.set(
        texts.map((text) => ({
          textId: text.id,
          title: text.title,
          excerpt: firstWords(text.content, 0, EXCERPT_WORD_COUNT),
          subscribed: text.subscribed,
        })),
      );
    });
  }

  search(): void {
    const query = this.searchQuery.trim();
    if (!query) {
      this.clearSearch();
      return;
    }

    this.searching.set(true);
    this.searchError.set(null);
    this.textService.search(query).subscribe({
      next: (results) => {
        this.searching.set(false);
        this.searchResults.set(results);
      },
      error: (err: HttpErrorResponse) => {
        this.searching.set(false);
        this.searchError.set(err.error?.error ?? 'Erreur lors de la recherche');
      },
    });
  }

  // Dismisses the search-results overlay (see home.html), undimming the
  // "Derniers textes" grid behind it — called both from its explicit close
  // button and when the query is cleared back to empty.
  clearSearch(): void {
    this.searchResults.set(null);
    this.searchError.set(null);
  }

  // Opens the "Clore un round" popin for one result instead of closing
  // immediately — the popin lets the user pick between closing right now or
  // scheduling the close for N days out.
  openCloseRoundPopup(target: ActionTarget): void {
    this.closeDays = 7;
    this.closeRoundPopup.set(target);
  }

  dismissCloseRoundPopup(): void {
    this.closeRoundPopup.set(null);
  }

  confirmCloseNow(target: ActionTarget): void {
    this.dismissCloseRoundPopup();

    const textId = target.textId;
    this.closingTextId.set(textId);
    this.closeErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.closeRound(textId).subscribe({
      next: () => {
        this.closingTextId.set(null);
        this.closedTextIds.update((ids) => new Set(ids).add(textId));
      },
      error: (err: HttpErrorResponse) => {
        this.closingTextId.set(null);
        this.closeErrors.update((errors) => ({
          ...errors,
          [textId]: err.error?.error ?? 'Erreur lors de la clôture du tour',
        }));
      },
    });
  }

  confirmScheduleClose(target: ActionTarget): void {
    const textId = target.textId;
    const days = this.closeDays;
    this.dismissCloseRoundPopup();

    this.closingTextId.set(textId);
    this.closeErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.scheduleClose(textId, days).subscribe({
      next: ({ scheduledCloseAt }) => {
        this.closingTextId.set(null);
        this.scheduledCloseAt.update((dates) => ({ ...dates, [textId]: scheduledCloseAt }));
      },
      error: (err: HttpErrorResponse) => {
        this.closingTextId.set(null);
        this.closeErrors.update((errors) => ({
          ...errors,
          [textId]: err.error?.error ?? 'Erreur lors de la programmation de la clôture',
        }));
      },
    });
  }

  // Subscribing is what reveals the Voter/Éditer/Clore un round/Supprimer
  // buttons for this target (see the template's [subscribed] checks) —
  // updating both lists in place is what makes them appear right away on
  // whichever one (search results, the "Derniers textes" grid, or both)
  // currently shows this text, without re-fetching either.
  subscribe(target: ActionTarget): void {
    const textId = target.textId;
    this.subscribingTextId.set(textId);
    this.subscribeErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.subscribe(textId).subscribe({
      next: () => {
        this.subscribingTextId.set(null);
        this.searchResults.update(
          (results) => results?.map((r) => (r.textId === textId ? { ...r, subscribed: true } : r)) ?? null,
        );
        this.recentTexts.update((cards) =>
          cards.map((c) => (c.textId === textId ? { ...c, subscribed: true } : c)),
        );
      },
      error: (err: HttpErrorResponse) => {
        this.subscribingTextId.set(null);
        this.subscribeErrors.update((errors) => ({
          ...errors,
          [textId]: err.error?.error ?? "Erreur lors de l'abonnement",
        }));
      },
    });
  }

  deleteText(target: ActionTarget): void {
    if (!confirm(`Supprimer le texte « ${target.title} » ? Cette action est irréversible.`)) return;

    const textId = target.textId;
    this.deletingTextId.set(textId);
    this.deleteErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.deleteText(textId).subscribe({
      next: () => {
        this.deletingTextId.set(null);
        // null (no search active) must stay null — filtering it into [] would
        // make the template's `@if (searchResults(); as results)` treat an
        // empty array as truthy and pop up the "no results" search panel
        // out of nowhere, even though nobody searched for anything.
        this.searchResults.update((results) => (results ? results.filter((r) => r.textId !== textId) : results));
        this.recentTexts.update((cards) => cards.filter((c) => c.textId !== textId));
      },
      error: (err: HttpErrorResponse) => {
        this.deletingTextId.set(null);
        this.deleteErrors.update((errors) => ({
          ...errors,
          [textId]: err.error?.error ?? 'Erreur lors de la suppression',
        }));
      },
    });
  }
}
