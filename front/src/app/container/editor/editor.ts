import { HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal, ViewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, RouterLink } from '@angular/router';
import { docFromText, docFromTextWithHighlights, EditorComponent, TextSelection } from '../../component/app-editor/app-editor';
import { AuthService } from '../../service/auth';
import { LastTextStorage } from '../../service/last-text-storage';
import { TextService } from '../../service/text';
import { firstWords, lastWords } from '../../util/words';

// Cycled through so each newly colored zone in the main text is visually
// distinct from the ones already there, instead of every zone using the
// same highlight color.
const HIGHLIGHT_PALETTE = ['#fef08a', '#bbf7d0', '#bfdbfe', '#fbcfe8', '#fed7aa', '#e9d5ff'];

// How much surrounding context the proposal panel shows above/below the
// selection being amended, in words.
const CONTEXT_WORD_COUNT = 100;

// True if [start,end) ranges a and b share any rune, but NOT for an exact
// match (a re-selection of an already-open zone joins it rather than
// conflicting) — same distinction queel's own resolveSlot makes server-side,
// checked here too so a doomed-to-be-rejected proposal never gets that far.
function overlapsPartially(a: { start: number; end: number }, b: { start: number; end: number }): boolean {
  if (a.start === b.start && a.end === b.end) return false;
  return a.start < b.end && a.end > b.start;
}

@Component({
  selector: 'editor-page',
  imports: [EditorComponent, RouterLink, FormsModule],
  templateUrl: './editor.html',
  styleUrl: './editor.css',
})
export class EditorPage {
  private readonly route = inject(ActivatedRoute);
  private readonly textService = inject(TextService);
  private readonly auth = inject(AuthService);
  private readonly lastTextStorage = inject(LastTextStorage);

  // Editable until the text is first saved (see save()); opening an
  // existing text via ?id= (e.g. from a search result) resolves it once the
  // fetch below completes, and it becomes read-only from then on.
  readonly title = signal('');

  // Gates the "Sauvegarder le texte" button — only relevant for the
  // create-a-new-text case (no savedTextId yet); see save().
  readonly canCreateText = signal(false);

  // Gates "Sélectionner du texte à amender": either permission lets a user
  // start a proposal (canSelect for a brand new range, canEditSelection for
  // one already open) — the editor can't know in advance which applies
  // until a range is actually picked, so either is enough to offer it.
  readonly canAmendText = signal(false);

  readonly loading = signal(false);
  readonly loadError = signal<string | null>(null);

  // The editor zone and the proposal zone are mutually exclusive (@if/@else
  // in the template), so exactly one <app-editor> is ever mounted at a time
  // — this single query resolves to whichever one is currently shown.
  @ViewChild(EditorComponent)
  private editorComponent?: EditorComponent;

  readonly lastSelection = signal<TextSelection | null>(null);
  readonly lastHighlightColor = signal<string | null>(null);
  private highlightCount = 0;

  // Every zone currently highlighted in the doc (loaded from existing open
  // slots, plus any created this session) — checked in highlight() so a
  // new selection that partially overlaps one is rejected client-side
  // instead of round-tripping to the server just to be told no.
  private selectedRanges: { start: number; end: number }[] = [];
  readonly selectionError = signal<string | null>(null);

  readonly saving = signal(false);
  readonly savedTextId = signal<string | null>(null);
  readonly saveError = signal<string | null>(null);

  // Doc JSON captured right before the editor zone is torn down, so the
  // <app-editor> that comes back once the proposal closes restores the same
  // content (including the highlight mark) instead of starting blank. Also
  // seeded from an existing text's content when opened via ?id=.
  readonly editorDocJSON = signal<unknown>(null);

  constructor() {
    this.auth.me().subscribe((me) => {
      this.canCreateText.set(me.root || me.permissions.canCreateText);
      this.canAmendText.set(me.root || me.permissions.canSelect || me.permissions.canEditSelection);
    });

    const id = this.route.snapshot.queryParamMap.get('id');
    if (id) {
      this.loading.set(true);
      this.textService.getWithSlots(id).subscribe({
        next: ({ text, slots }) => {
          this.loading.set(false);
          this.title.set(text.title);
          const ranges = slots.map((slot, i) => ({
            start: slot.start,
            end: slot.end,
            color: HIGHLIGHT_PALETTE[i % HIGHLIGHT_PALETTE.length],
          }));
          this.highlightCount = ranges.length;
          this.selectedRanges = ranges.map(({ start, end }) => ({ start, end }));
          this.editorDocJSON.set(docFromTextWithHighlights(text.content, ranges));
          this.savedTextId.set(text.id);
        },
        error: (err: HttpErrorResponse) => {
          this.loading.set(false);
          this.loadError.set(err.error?.error ?? 'Erreur lors du chargement du texte');
        },
      });
    }
  }

  // The sliding proposal panel: opened by highlight(), holds the [start, end)
  // rune range the selection maps to so submitProposal() can call the slot
  // API with it once the user has edited the proposal editor's content.
  // proposalDoc seeds that editor with the selected text, one paragraph per
  // line, via the same [initialDoc] mechanism as the main editor.
  readonly proposal = signal<{ start: number; end: number } | null>(null);
  readonly proposalDoc = signal<unknown>(null);
  readonly proposing = signal(false);
  readonly proposalError = signal<string | null>(null);

  // The CONTEXT_WORD_COUNT words right before/after the selection in the
  // full text, captured at highlight() time (the main editor is torn down
  // once the proposal panel opens, so its content isn't reachable then).
  readonly contextBefore = signal('');
  readonly contextAfter = signal('');

  highlight(): void {
    if (!this.editorComponent) return;

    const pending = this.editorComponent.getSelection();
    if (!pending) return;

    this.selectionError.set(null);
    if (this.selectedRanges.some((r) => overlapsPartially(r, pending))) {
      this.selectionError.set('Cette sélection chevauche une zone déjà sélectionnée.');
      return;
    }

    const color = HIGHLIGHT_PALETTE[this.highlightCount % HIGHLIGHT_PALETTE.length];
    const selection = this.editorComponent.highlightSelection(color);
    this.lastSelection.set(selection);
    if (selection) {
      this.highlightCount++;
      this.selectedRanges.push({ start: selection.start, end: selection.end });
      this.lastHighlightColor.set(color);
      const fullText = this.editorComponent.getText();
      this.contextBefore.set(lastWords(fullText, selection.start, CONTEXT_WORD_COUNT));
      this.contextAfter.set(firstWords(fullText, selection.end, CONTEXT_WORD_COUNT));
      this.editorDocJSON.set(this.editorComponent.getDocJSON());
      this.proposal.set({ start: selection.start, end: selection.end });
      this.proposalDoc.set(docFromText(selection.text));
      this.proposalError.set(null);
    }
  }

  closeProposal(): void {
    this.proposal.set(null);
    this.proposalDoc.set(null);
    this.proposalError.set(null);
    this.contextBefore.set('');
    this.contextAfter.set('');
  }

  submitProposal(): void {
    const range = this.proposal();
    const textId = this.lastTextStorage.read()?.id ?? null;
    if (!range || !textId || !this.editorComponent) return;

    const content = this.editorComponent.getText();

    this.proposing.set(true);
    this.proposalError.set(null);
    this.textService.proposeEdit(textId, range.start, range.end, content).subscribe({
      next: () => {
        this.proposing.set(false);
        this.closeProposal();
      },
      error: (err: HttpErrorResponse) => {
        this.proposing.set(false);
        this.proposalError.set(err.error?.error ?? 'Erreur lors de la proposition');
      },
    });
  }

  // Only ever called for a brand-new text — the button that triggers it
  // (see editor.html) disappears for good once the text has been saved
  // once, so there's no "update" branch to speak of any more.
  save(): void {
    if (!this.editorComponent) return;
    this.saving.set(true);
    this.saveError.set(null);

    const content = this.editorComponent.getText();
    this.textService.create(this.title(), content).subscribe({
      next: (response) => {
        this.saving.set(false);
        this.savedTextId.set(response.id);
        this.lastTextStorage.write({ id: response.id, title: this.title() });
      },
      error: (err: HttpErrorResponse) => {
        this.saving.set(false);
        this.saveError.set(err.error?.error ?? "Erreur lors de la sauvegarde");
      },
    });
  }
}
