import { Location } from '@angular/common';
import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, WritableSignal, inject, signal } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { forkJoin } from 'rxjs';
import { Fragment, Slot } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';

// authorId queel stamps on the fragment it automatically seeds a new slot
// with (the untouched original content, as the implicit "keep as-is"
// competitor) — see queel.SeedAuthorID. It isn't listed as a proposal to
// diff against itself; instead it's the target of each zone's "vote for
// the original" button.
const SEED_AUTHOR_ID = 'seed';

interface FragmentVote {
  fragment: Fragment;
  // Drives this proposal's vote popup — set true only while the pointer is
  // over its own word in the "valeurs modifiées" line (or the popup
  // itself, so moving onto its buttons doesn't close it). The "valeurs
  // initiales" line is never interactive: voting for the original is what
  // group.seedFragmentId is for, offered alongside each proposal's own
  // popup instead. See vote.html's mouseenter/mouseleave bindings.
  hovering: WritableSignal<boolean>;
}

interface SlotGroup {
  slot: Slot;
  original: string;
  // The slot's seed fragment id, voted for by the "texte original" button —
  // null only if queel somehow didn't seed this slot (shouldn't happen).
  seedFragmentId: string | null;
  fragments: FragmentVote[];
  // A slot has at most one active vote per user (queel: choosing a
  // different fragment for the same slot withdraws the previous one), so
  // voting state lives once per slot, not per fragment.
  voting: WritableSignal<boolean>;
  votedFragmentId: WritableSignal<string | null>;
  error: WritableSignal<string | null>;
}

// The whole text, reconstructed once as an ordered sequence of plain
// stretches and slots — rather than one isolated card per slot — so a text
// with several slots reads as one continuous sentence with each slot shown
// in place, instead of repeating most of the sentence once per slot.
type Segment = { kind: 'plain'; text: string } | { kind: 'slot'; group: SlotGroup };

@Component({
  selector: 'vote-page',
  imports: [],
  templateUrl: './vote.html',
})
export class VotePage implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);
  private readonly location = inject(Location);
  private readonly textService = inject(TextService);
  private readonly auth = inject(AuthService);

  readonly loading = signal(true);
  readonly loadError = signal<string | null>(null);

  readonly textTitle = signal('');
  readonly slotGroups = signal<SlotGroup[]>([]);
  readonly segments = signal<Segment[]>([]);

  // Hover-to-vote controls only ever show if the caller actually has the
  // right to vote — otherwise hovering a zone reveals nothing.
  readonly canVote = signal(false);

  // Whether this user's vote in that slot went to that fragment — what
  // paints the wording they chose (see vote.html).
  //
  // Takes a nullable id so the "valeurs initiales" line can ask about
  // group.seedFragmentId directly: a slot queel somehow didn't seed has a
  // null one, and null must never match a null vote.
  hasVotedFor(group: SlotGroup, fragmentId: string | null): boolean {
    return fragmentId !== null && group.votedFragmentId() === fragmentId;
  }

  // Back to wherever this page was opened from — the home thread, the
  // notification inbox — rather than always to /home, which silently
  // discards the screen the reader was on. There are two ways in already
  // (see home.html and notifications.ts), and a fixed destination is wrong
  // for one of them whichever one it names.
  //
  // Falls back to /home when there is no in-app history to step into: the
  // page was opened straight from its URL, where location.back() would
  // leave the app entirely.
  goBack(): void {
    if (this.router.lastSuccessfulNavigation()?.previousNavigation) {
      this.location.back();
      return;
    }
    this.router.navigateByUrl('/home');
  }

  ngOnInit(): void {
    this.auth.me().subscribe((me) => {
      this.canVote.set(me.root || me.permissions.canVote);
    });

    const id = this.route.snapshot.queryParamMap.get('id');
    if (!id) {
      this.loading.set(false);
      this.loadError.set("Aucun id de texte fourni dans l'url (?id=...).");
      return;
    }

    this.textService.getWithSlots(id).subscribe({
      next: ({ text, slots }) => {
        this.textTitle.set(text.title);
        if (slots.length === 0) {
          this.loading.set(false); // no open round — normal state, not an error
          return;
        }
        this.loadFragments(id, text.content, slots);
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.loadError.set(err.error?.error ?? 'Erreur lors du chargement du texte');
      },
    });
  }

  private loadFragments(textId: string, content: string, slots: Slot[]): void {
    const runes = Array.from(content);

    // The caller's existing votes ride along with the fragments: both are
    // needed before the first render, and asking for them afterwards would
    // paint the page once without the highlight and again with it.
    forkJoin({
      fragmentsPerSlot: forkJoin(
        slots.map((slot) => this.textService.fragmentsForSlot(textId, slot.id)),
      ),
      myVotes: this.textService.myVotes(textId),
    }).subscribe({
      next: ({ fragmentsPerSlot, myVotes }) => {
        this.loading.set(false);
        const groups: SlotGroup[] = slots.map((slot, i) => {
          const original = runes.slice(slot.start, slot.end).join('');
          const allFragments = fragmentsPerSlot[i];
          const seed = allFragments.find((f) => f.authorId === SEED_AUTHOR_ID);
          const proposals = allFragments.filter((f) => f.authorId !== SEED_AUTHOR_ID);

          return {
            slot,
            original,
            seedFragmentId: seed?.id ?? null,
            fragments: proposals.map((fragment) => ({
              fragment,
              hovering: signal(false),
            })),
            voting: signal(false),
            // Seeded from the server, so a vote cast in an earlier visit
            // is highlighted on load rather than only until the next
            // reload.
            votedFragmentId: signal<string | null>(myVotes[slot.id] ?? null),
            error: signal<string | null>(null),
          };
        });

        this.slotGroups.set(groups);
        this.segments.set(this.buildSegments(runes, groups));
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.loadError.set(err.error?.error ?? 'Erreur lors du chargement des propositions');
      },
    });
  }

  // Pending close-popup timers, keyed by fragment — see showPopup/
  // hidePopupSoon: closing on mouseleave is delayed rather than immediate,
  // so moving the pointer from a fragment's word down to its popup (across
  // the small visual gap between them) doesn't get cut short by the popup
  // vanishing before the pointer actually arrives.
  private readonly hideTimeouts = new Map<FragmentVote, ReturnType<typeof setTimeout>>();

  // The one fragment whose popup showPopup most recently opened — at most
  // one should ever be visible at a time. Without this, quickly sweeping
  // the pointer from one fragment's word straight to another's (skipping
  // the trip through the first one's own popup) left the first popup's
  // grace period still pending, so both stayed visible together for a
  // moment instead of the first vanishing as soon as the second opens.
  private activeFragment: FragmentVote | null = null;

  // Called on mouseenter of either a fragment's word or its own popup —
  // cancels any close already queued by hidePopupSoon, so briefly leaving
  // one for the other doesn't lose the popup in between. Also immediately
  // closes whichever other fragment's popup was previously open, since
  // only one is ever meant to be visible at once.
  showPopup(fv: FragmentVote): void {
    this.clearHideTimeout(fv);
    if (this.activeFragment && this.activeFragment !== fv) {
      this.hidePopupNow(this.activeFragment);
    }
    this.activeFragment = fv;
    fv.hovering.set(true);
  }

  // Called on mouseleave of either a fragment's word or its own popup —
  // waits a short moment before actually closing instead of closing
  // immediately, precisely to tolerate that in-between gap.
  hidePopupSoon(fv: FragmentVote): void {
    this.clearHideTimeout(fv);
    this.hideTimeouts.set(
      fv,
      setTimeout(() => {
        fv.hovering.set(false);
        if (this.activeFragment === fv) {
          this.activeFragment = null;
        }
      }, 250),
    );
  }

  // Called right after casting a vote, and to force-close whichever
  // fragment's popup was open when another one is about to show — no grace
  // period, either the interaction is already done or another popup is
  // taking its place.
  hidePopupNow(fv: FragmentVote): void {
    this.clearHideTimeout(fv);
    fv.hovering.set(false);
    if (this.activeFragment === fv) {
      this.activeFragment = null;
    }
  }

  private clearHideTimeout(fv: FragmentVote): void {
    const pending = this.hideTimeouts.get(fv);
    if (pending !== undefined) {
      clearTimeout(pending);
      this.hideTimeouts.delete(fv);
    }
  }

  // Walks slots left to right (queel guarantees they never overlap within a
  // round), interleaving the plain document text that falls between/around
  // them — so the template can render one continuous reconstructed sentence
  // instead of one isolated snippet per slot.
  private buildSegments(runes: string[], groups: SlotGroup[]): Segment[] {
    const sorted = [...groups].sort((a, b) => a.slot.start - b.slot.start);
    const segments: Segment[] = [];
    let cursor = 0;
    for (const group of sorted) {
      if (group.slot.start > cursor) {
        segments.push({ kind: 'plain', text: runes.slice(cursor, group.slot.start).join('') });
      }
      segments.push({ kind: 'slot', group });
      cursor = group.slot.end;
    }
    if (cursor < runes.length) {
      segments.push({ kind: 'plain', text: runes.slice(cursor).join('') });
    }
    return segments;
  }

  vote(group: SlotGroup, fragmentId: string | null): void {
    if (!fragmentId || group.voting()) return;

    group.voting.set(true);
    group.error.set(null);
    this.textService.castVote(fragmentId).subscribe({
      next: () => {
        group.voting.set(false);
        group.votedFragmentId.set(fragmentId);
      },
      error: (err: HttpErrorResponse) => {
        group.voting.set(false);
        group.error.set(err.error?.error ?? 'Erreur lors du vote');
      },
    });
  }
}
