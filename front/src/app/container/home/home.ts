import { DatePipe, DecimalPipe } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { HistoryVersion, RecentText, SearchResult, TagCount } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';
import { firstWords } from '../../util/words';

// One page of the thread. The column scrolls, so this is a batch size
// rather than a screenful — it matches the API's own default limit.
const RECENT_TEXTS_COUNT = 20;

// Enough words to tell two texts apart in the thread's two-line preview.
// The reading pane shows the whole content, so this is only ever a label.
const EXCERPT_WORD_COUNT = 30;

// One entry of the thread, whether it came from the recent listing or from
// a search. Unifying them is what lets the thread column and the reading
// pane be written once instead of once per source — the two responses
// differ only in what they happen to carry, not in what the page does with
// them.
export interface ThreadItem {
  textId: string;
  title: string;
  excerpt: string;
  subscribed: boolean;
  tags: string[];

  // The whole text, for the reading pane. Null for a search result: that
  // response carries no content, so it is fetched when the item is first
  // selected and kept here afterwards (see select).
  content: string | null;

  // Search-only. 0/undefined means "no open round" and "not from a search"
  // alike, which is why the template tests them rather than the source.
  roundNumber?: number;
  score?: number;
}

// The shape every subscribe/close-round/delete action needs. ThreadItem
// satisfies it, so the action methods don't depend on where the entry came
// from.
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
  readonly searchError = signal<string | null>(null);

  readonly recentTexts = signal<ThreadItem[]>([]);

  // The labels on offer, and the one being filtered on — empty for none.
  // A single active label rather than a set: narrowing by two at once is a
  // question nobody asked, and it would need the api to answer it.
  readonly tags = signal<TagCount[]>([]);
  // The labels being crossed, as one raw line. Sent to the api unsplit:
  // reading a line into labels is one rule and it lives there — splitting
  // it here would let "loi vote" mean two labels on this screen and one
  // everywhere else.
  readonly tagLine = signal('');

  // Null when no search is active — distinct from an empty array, which
  // means "searched, found nothing" and must show that rather than
  // silently falling back to the recent listing.
  readonly searchResults = signal<ThreadItem[] | null>(null);

  // What the thread column actually renders: search results when there are
  // some, the recent listing otherwise.
  readonly thread = computed(() => this.searchResults() ?? this.recentTexts());

  readonly selectedId = signal<string | null>(null);

  // Derived rather than stored, so an item patched in place (its content
  // fetched, its subscription flipped) is reflected in the reading pane
  // without having to remember to update a second copy of it.
  readonly selected = computed(
    () => this.thread().find((item) => item.textId === this.selectedId()) ?? null,
  );

  readonly loadingContent = signal(false);
  readonly contentError = signal<string | null>(null);

  // Every version of the selected text, oldest first — the chain the
  // arrows walk. Loaded with the selection rather than on demand now that
  // navigating it is a primary control and not a panel to open.
  readonly versions = signal<HistoryVersion[]>([]);

  // Which link of that chain the pane is showing. -1 while the chain is
  // unknown, where the pane falls back to the selected text's own content
  // — which is that chain's last link anyway.
  readonly viewedIndex = signal(-1);

  readonly viewedVersion = computed(() => this.versions()[this.viewedIndex()] ?? null);

  // What the pane renders: the version being read, or the selected text
  // while the chain hasn't arrived.
  readonly readingContent = computed(
    () => this.viewedVersion()?.content ?? this.selected()?.content ?? null,
  );

  // True only while an *older* version is on screen — the current one is
  // where the reader already was, not a trip into the past.
  readonly readingPastVersion = computed(() => {
    const viewed = this.viewedVersion();
    return viewed !== null && viewed.textId !== this.selectedId();
  });

  // What the round of the displayed version settled: per slot, the wording
  // it replaced and the one the votes carried. Empty for a round nobody
  // has proposed against — typically the open one on the current version.
  readonly displayedSlots = computed(() =>
    (this.viewedVersion()?.rounds ?? []).flatMap((round) => round.slots),
  );

  // Whether the displayed version's round has anything to vote on.
  //
  // Only trusted once the chain has actually arrived: before that, and if
  // the request failed, the answer is unknown — and "unknown" must not be
  // read as "nothing", or a failed history call would take the vote away.
  readonly voteAvailable = computed(
    () => this.versions().length === 0 || this.displayedSlots().length > 0,
  );

  readonly canGoPreviousVersion = computed(() => this.viewedIndex() > 0);
  readonly canGoNextVersion = computed(() => {
    const index = this.viewedIndex();
    return index >= 0 && index < this.versions().length - 1;
  });

  // The round the displayed version is on. Each archived version carries
  // the round that closed on it; the current one carries the round open
  // now, which the listing already told us.
  readonly displayedRoundNumber = computed(() => {
    const viewed = this.viewedVersion();
    if (viewed && viewed.textId !== this.selectedId()) {
      return viewed.rounds.at(-1)?.number ?? null;
    }
    return this.selected()?.roundNumber ?? null;
  });

  // Infinite scroll for the thread: hasMore is false once a page comes back
  // shorter than requested (including empty). loadingMore guards against
  // firing several requests from rapid-fire scroll events for the same page.
  readonly loadingMoreRecentTexts = signal(false);
  readonly hasMoreRecentTexts = signal(true);

  // The rbac permissions gating each action. They are the only thing that
  // hides an action now: following a text no longer gates them, so the
  // reading pane offers whatever the user is actually allowed to do.
  readonly canAmendText = signal(false);
  readonly canVote = signal(false);
  readonly canCloseText = signal(false);

  // Deleting a text outright reuses the canCreateText permission as its
  // bar (see api's deleteTextHandler) — whoever can create texts can also
  // remove one from here, rather than a dedicated permission bit.
  readonly canDeleteText = signal(false);

  readonly canSubscribe = signal(false);

  // Per-text action state. Still keyed by textId rather than collapsed to
  // a single value now that only one text is acted on at a time: an action
  // outlives the selection that started it, and a result arriving after
  // the user has moved on must land on the text it belongs to.
  readonly subscribingTextId = signal<string | null>(null);
  readonly subscribeErrors = signal<Readonly<Record<string, string>>>({});

  readonly closingTextId = signal<string | null>(null);
  readonly closeErrors = signal<Readonly<Record<string, string>>>({});

  // The version a close just produced, so the pane can say why the text it
  // is showing suddenly changed. Cleared as soon as another text is
  // selected — it is news about one close, not a property of the text.
  readonly justClosedInto = signal<string | null>(null);

  // The "Clore un round" popin: which text it's open for (null = closed),
  // and the day count bound to its "dans N jours" input.
  readonly closeRoundPopup = signal<ActionTarget | null>(null);
  closeDays = 7;

  // Set once scheduleClose succeeds, so the pane can show when the round is
  // due to close on its own instead of (or alongside) the manual button.
  readonly scheduledCloseAt = signal<Readonly<Record<string, string>>>({});

  readonly deletingTextId = signal<string | null>(null);
  readonly deleteErrors = signal<Readonly<Record<string, string>>>({});

  ngOnInit(): void {
    this.auth.me().subscribe((me) => {
      this.canAmendText.set(me.root || me.permissions.canEditText);
      this.canVote.set(me.root || me.permissions.canVote);
      this.canCloseText.set(me.root || me.permissions.canCloseText);
      this.canDeleteText.set(me.root || me.permissions.canCreateText);
      this.canSubscribe.set(me.root || me.permissions.canSubscribe);
    });
    this.loadRecentTexts();
    this.loadTags();
  }

  // Silent on failure: losing the labels costs the filter, never the
  // listing they would have narrowed.
  private loadTags(): void {
    this.textService.tags().subscribe({
      next: (tags) => this.tags.set(tags),
      error: () => this.tags.set([]),
    });
  }

  // One value carrying the whole line, which the api reads into labels.
  private queryTags(): string[] {
    const line = this.tagLine().trim();
    return line === '' ? [] : [line];
  }

  clearTags(): void {
    if (this.tagLine() === '') return;
    this.tagLine.set('');
    this.applyTagFilter();
  }

  applyTagFilter(): void {
    this.selectedId.set(null);
    this.loadRecentTexts();
  }

  // Accents folded away, so typing "ecolo" finds "écologie". Labels keep
  // theirs — they are what gets stored and shown — and only the comparison
  // drops them: nobody reaches for the accented key while filtering, and a
  // completion that demands it never fires.
  private static fold(value: string): string {
    return value
      .toLowerCase()
      .normalize('NFD')
      .replace(/\p{Diacritic}/gu, '');
  }

  // Splits the line the way the eye does, for the completion only. Never
  // for the query: that goes whole to the api, so this approximation
  // cannot make the filter disagree with the server about what was asked.
  private static words(line: string): string[] {
    return line.split(/[#,;\s]+/).filter((word) => word !== '');
  }

  private static onSeparator(line: string): boolean {
    return line === '' || /[#,;\s]$/.test(line);
  }

  // Labels matching what is being typed, minus those already on the line —
  // suggesting one twice offers a narrowing that would change nothing.
  //
  // Matched anywhere rather than only at the start, since someone after
  // "loi-de-finances" is as likely to type "finances"; those beginning
  // with what was typed come first, which is what a reader expects on top.
  readonly tagSuggestions = computed(() => {
    const line = this.tagLine();
    const words = HomePage.words(line.toLowerCase());
    const onSeparator = HomePage.onSeparator(line);
    const current = HomePage.fold(onSeparator ? '' : (words.at(-1) ?? ''));
    const already = new Set(words.slice(0, words.length - (onSeparator ? 0 : 1)));

    return this.tags()
      .filter((tag) => !already.has(tag.tag))
      .filter((tag) => current === '' || HomePage.fold(tag.tag).includes(current))
      .sort((a, b) => {
        const aStarts = HomePage.fold(a.tag).startsWith(current);
        const bStarts = HomePage.fold(b.tag).startsWith(current);
        return aStarts === bStarts ? 0 : aStarts ? -1 : 1;
      })
      .slice(0, 8);
  });

  // Replaces the word being typed with the label chosen, leaving a space so
  // the next one can follow without reaching for the mouse again.
  completeTag(tag: string): void {
    const line = this.tagLine();
    const words = HomePage.words(line);
    if (!HomePage.onSeparator(line)) words.pop();

    this.tagLine.set([...words, tag].join(' ') + ' ');
    this.applyTagFilter();
  }

  private toThreadItem(text: RecentText): ThreadItem {
    return {
      textId: text.id,
      title: text.title,
      excerpt: firstWords(text.content, 0, EXCERPT_WORD_COUNT),
      subscribed: text.subscribed,
      tags: text.tags ?? [],
      content: text.content,
      roundNumber: text.roundNumber,
    };
  }

  private searchResultToThreadItem(result: SearchResult): ThreadItem {
    return {
      textId: result.textId,
      title: result.title,
      // The search response carries no content, so there is nothing to
      // excerpt — the title and score are what a result has to show for
      // itself until it's selected.
      excerpt: '',
      subscribed: result.subscribed,
      // The search response carries no tags either; they arrive with the
      // content when the result is selected.
      tags: [],
      content: null,
      roundNumber: result.roundNumber,
      score: result.score,
    };
  }

  private loadRecentTexts(): void {
    this.hasMoreRecentTexts.set(true);
    this.recentTexts.set([]);
    this.textService.listRecent(RECENT_TEXTS_COUNT, 0, this.queryTags()).subscribe((texts) => {
      const items = texts.map((text) => this.toThreadItem(text));
      this.recentTexts.set(items);
      this.hasMoreRecentTexts.set(texts.length === RECENT_TEXTS_COUNT);
      // Land on something readable rather than an empty pane — the newest
      // text, which is what the thread opens on anyway.
      if (!this.selectedId() && items.length > 0) this.select(items[0]);
    });
  }

  loadMoreRecentTexts(): void {
    // Nothing to page through while a search is showing: the thread is
    // then the result set, which came whole.
    if (this.searchResults() || this.loadingMoreRecentTexts() || !this.hasMoreRecentTexts()) return;

    this.loadingMoreRecentTexts.set(true);
    const offset = this.recentTexts().length;
    this.textService.listRecent(RECENT_TEXTS_COUNT, offset, this.queryTags()).subscribe({
      next: (texts) => {
        this.loadingMoreRecentTexts.set(false);
        this.hasMoreRecentTexts.set(texts.length === RECENT_TEXTS_COUNT);
        this.recentTexts.update((items) => [
          ...items,
          ...texts.map((text) => this.toThreadItem(text)),
        ]);
      },
      error: () => {
        this.loadingMoreRecentTexts.set(false);
      },
    });
  }

  // Triggers loadMoreRecentTexts once the thread column (not the page) is
  // scrolled within 100px of its bottom.
  onThreadScroll(event: Event): void {
    const el = event.target as HTMLElement;
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 100) {
      this.loadMoreRecentTexts();
    }
  }

  // Shows one thread entry in the reading pane, fetching its content the
  // first time if the entry came from a search and doesn't carry any.
  select(item: ThreadItem): void {
    this.selectedId.set(item.textId);
    this.contentError.set(null);
    // Belongs to the text that was showing, not to this one.
    this.loadVersions(item.textId);
    if (item.textId !== this.justClosedInto()) this.justClosedInto.set(null);

    if (item.content !== null) return;

    this.loadingContent.set(true);
    this.textService.get(item.textId).subscribe({
      next: (text) => {
        this.loadingContent.set(false);
        this.patchItem(item.textId, {
          content: text.content,
          excerpt: firstWords(text.content, 0, EXCERPT_WORD_COUNT),
        });
      },
      error: () => {
        this.loadingContent.set(false);
        this.contentError.set('Chargement du texte impossible.');
      },
    });
  }

  // Fetches the chain of versions for a text and lands on its current
  // one, which is where the reader is.
  //
  // Silent on failure: the arrows simply stay disabled. Losing the ability
  // to walk the history must not disturb reading the text itself.
  private loadVersions(textId: string): void {
    this.versions.set([]);
    this.viewedIndex.set(-1);

    this.textService.history(textId).subscribe({
      next: (versions) => {
        // Guard against a slower response for a text the reader has since
        // left: it would otherwise replace the chain of the current one.
        if (this.selectedId() !== textId) return;

        this.versions.set(versions);
        this.viewedIndex.set(versions.findIndex((version) => version.textId === textId));
      },
      error: () => {
        this.versions.set([]);
        this.viewedIndex.set(-1);
      },
    });
  }

  previousVersion(): void {
    if (this.canGoPreviousVersion()) this.viewedIndex.update((index) => index - 1);
  }

  nextVersion(): void {
    if (this.canGoNextVersion()) this.viewedIndex.update((index) => index + 1);
  }

  // Applies a change to whichever lists hold this text. Both, because a
  // text can be in the recent listing and in the current search results at
  // once, and the two must not disagree about whether it's followed.
  private patchItem(textId: string, patch: Partial<ThreadItem>): void {
    const apply = (items: ThreadItem[]) =>
      items.map((item) => (item.textId === textId ? { ...item, ...patch } : item));

    this.recentTexts.update(apply);
    this.searchResults.update((items) => (items ? apply(items) : items));
  }

  // Swaps one entry for another in place, keeping its position in the
  // thread — a new version of a text that was second in the list is still
  // second, not suddenly first.
  private replaceItem(textId: string, replacement: ThreadItem): void {
    const apply = (items: ThreadItem[]) =>
      items.map((item) => (item.textId === textId ? replacement : item));

    this.recentTexts.update(apply);
    this.searchResults.update((items) => (items ? apply(items) : items));
  }

  private removeItem(textId: string): void {
    this.recentTexts.update((items) => items.filter((item) => item.textId !== textId));
    // null (no search active) must stay null — filtering it into [] would
    // make the template treat it as "searched, found nothing".
    this.searchResults.update((items) =>
      items ? items.filter((item) => item.textId !== textId) : items,
    );

    // The pane would otherwise sit empty on a text that no longer exists.
    if (this.selectedId() === textId) {
      const next = this.thread()[0] ?? null;
      this.selectedId.set(next?.textId ?? null);
    }
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
        const items = results.map((result) => this.searchResultToThreadItem(result));
        this.searchResults.set(items);
        // Follow the thread: the pane shows whatever the column is
        // showing, so a search that lands on nothing leaves it empty
        // rather than on a text no longer in view.
        if (items.length > 0) this.select(items[0]);
        else this.selectedId.set(null);
      },
      error: (err: HttpErrorResponse) => {
        this.searching.set(false);
        this.searchError.set(err.error?.error ?? 'Erreur lors de la recherche');
      },
    });
  }

  // Returns the thread to the recent listing.
  clearSearch(): void {
    this.searchQuery = '';
    this.searchResults.set(null);
    this.searchError.set(null);

    const items = this.recentTexts();
    if (items.length > 0 && !items.some((item) => item.textId === this.selectedId())) {
      this.select(items[0]);
    }
  }

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
      next: (outcome) => {
        this.closingTextId.set(null);
        // Closing forks: the text that was on screen is now frozen history
        // and drops out of the recent listing, so leaving it in the thread
        // would show something the next reload won't have. Its place is
        // taken by the version it produced, which is then selected — the
        // reader stays on the text they were reading, at its new version.
        this.replaceItem(textId, {
          textId: outcome.text.id,
          title: outcome.text.title,
          excerpt: firstWords(outcome.text.content, 0, EXCERPT_WORD_COUNT),
          subscribed: true,
          tags: outcome.text.tags ?? [],
          content: outcome.text.content,
          // The fork opens the round after the one that just closed, and
          // the response only names the closed one.
          roundNumber: outcome.round.number + 1,
        });
        this.selectedId.set(outcome.text.id);
        this.justClosedInto.set(outcome.text.id);
        // The chain just grew a link, and the reader is on the new one.
        this.loadVersions(outcome.text.id);
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

  subscribe(target: ActionTarget): void {
    const textId = target.textId;
    this.subscribingTextId.set(textId);
    this.subscribeErrors.update(({ [textId]: _dropped, ...rest }) => rest);

    this.textService.subscribe(textId).subscribe({
      next: () => {
        this.subscribingTextId.set(null);
        this.patchItem(textId, { subscribed: true });
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
        this.removeItem(textId);
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
