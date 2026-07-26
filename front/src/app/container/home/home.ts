import { DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { SearchResult } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';
import { firstWords } from '../../util/words';

const RECENT_TEXTS_COUNT = 4;
const EXCERPT_WORD_COUNT = 100;

export interface RecentTextCard {
  id: string;
  title: string;
  excerpt: string;
}

@Component({
  selector: 'home-page',
  imports: [FormsModule, DecimalPipe, RouterLink],
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
  // use anyway.
  readonly canAmendText = signal(false);
  readonly canVote = signal(false);
  readonly canCloseText = signal(false);

  // Deleting a text outright is root-only (see api's deleteTextHandler) —
  // not gated by any of the canX permissions above, same bar as the
  // Backoffice link in the header.
  readonly isRoot = signal(false);

  // Per-result close-round state, keyed by textId — a search list can hold
  // several results at once, each closeable independently.
  readonly closingTextId = signal<string | null>(null);
  readonly closedTextIds = signal<ReadonlySet<string>>(new Set());
  readonly closeErrors = signal<Readonly<Record<string, string>>>({});

  // Per-result delete state, same shape as close-round above.
  readonly deletingTextId = signal<string | null>(null);
  readonly deleteErrors = signal<Readonly<Record<string, string>>>({});

  ngOnInit(): void {
    this.auth.me().subscribe((me) => {
      this.canAmendText.set(me.root || me.permissions.canSelect || me.permissions.canEditSelection);
      this.canVote.set(me.root || me.permissions.canVote);
      this.canCloseText.set(me.root || me.permissions.canCloseText);
      this.isRoot.set(me.root);
    });
    this.loadRecentTexts();
  }

  private loadRecentTexts(): void {
    this.textService.listRecent(RECENT_TEXTS_COUNT).subscribe((texts) => {
      this.recentTexts.set(
        texts.map((text) => ({
          id: text.id,
          title: text.title,
          excerpt: firstWords(text.content, 0, EXCERPT_WORD_COUNT),
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

  closeRound(result: SearchResult): void {
    const textId = result.textId;
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

  deleteText(result: SearchResult): void {
    if (!confirm(`Supprimer le texte « ${result.title} » ? Cette action est irréversible.`)) return;

    const textId = result.textId;
    this.deletingTextId.set(textId);
    this.deleteErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.deleteText(textId).subscribe({
      next: () => {
        this.deletingTextId.set(null);
        this.searchResults.update((results) => (results ?? []).filter((r) => r.textId !== textId));
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
