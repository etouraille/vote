import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, WritableSignal, inject, signal } from '@angular/core';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { forkJoin } from 'rxjs';
import { Fragment, Slot } from '../../model/text.model';
import { AuthService } from '../../service/auth';
import { TextService } from '../../service/text';
import { DiffToken, diffWords } from './diff';

// authorId queel stamps on the fragment it automatically seeds a new slot
// with (the untouched original content, as the implicit "keep as-is"
// competitor) — see queel.SeedAuthorID. It isn't listed as a proposal to
// diff against itself; instead it's the target of each zone's "vote for
// the original" button.
const SEED_AUTHOR_ID = 'seed';

interface FragmentDiff {
  fragment: Fragment;
  diff: DiffToken[];
}

interface SlotGroup {
  slot: Slot;
  original: string;
  // The slot's seed fragment id, voted for by the "texte original" button —
  // null only if queel somehow didn't seed this slot (shouldn't happen).
  seedFragmentId: string | null;
  fragments: FragmentDiff[];
  // A slot has at most one active vote per user (queel: choosing a
  // different fragment for the same slot withdraws the previous one), so
  // voting state lives once per slot, not per fragment.
  voting: WritableSignal<boolean>;
  votedFragmentId: WritableSignal<string | null>;
  error: WritableSignal<string | null>;
}

@Component({
  selector: 'vote-page',
  imports: [RouterLink],
  templateUrl: './vote.html',
})
export class VotePage implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly textService = inject(TextService);
  private readonly auth = inject(AuthService);

  readonly loading = signal(true);
  readonly loadError = signal<string | null>(null);

  readonly textTitle = signal('');
  readonly slotGroups = signal<SlotGroup[]>([]);

  // Hover-to-vote controls only ever show if the caller actually has the
  // right to vote — otherwise hovering a zone reveals nothing.
  readonly canVote = signal(false);

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

    forkJoin(slots.map((slot) => this.textService.fragmentsForSlot(textId, slot.id))).subscribe({
      next: (fragmentsPerSlot) => {
        this.loading.set(false);
        this.slotGroups.set(
          slots.map((slot, i) => {
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
                diff: diffWords(original, fragment.content),
              })),
              voting: signal(false),
              votedFragmentId: signal<string | null>(null),
              error: signal<string | null>(null),
            };
          }),
        );
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.loadError.set(err.error?.error ?? 'Erreur lors du chargement des propositions');
      },
    });
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
