import {
  AfterViewInit,
  Component,
  ElementRef,
  Input,
  ViewChild
} from '@angular/core';

import { Node as PMNode, Schema } from "prosemirror-model";
import { EditorState } from "prosemirror-state";
import { EditorView } from "prosemirror-view";
import { schema as basicSchema } from "prosemirror-schema-basic";

export const DEFAULT_HIGHLIGHT_COLOR = '#fef08a';

const schema = new Schema({
  nodes: basicSchema.spec.nodes,
  marks: basicSchema.spec.marks.addToEnd('highlight', {
    attrs: { color: { default: DEFAULT_HIGHLIGHT_COLOR } },
    parseDOM: [
      {
        tag: 'span.highlight',
        getAttrs: (dom) => ({ color: (dom as HTMLElement).style.backgroundColor || DEFAULT_HIGHLIGHT_COLOR }),
      },
    ],
    toDOM: (mark) => ['span', { class: 'highlight', style: `background-color: ${mark.attrs['color']}` }, 0],
  }),
});

/** Builds an [initialDoc] value from plain text, one paragraph per line —
 *  for seeding an <app-editor> with a string instead of a serialized doc. */
export function docFromText(text: string): unknown {
  return {
    type: 'doc',
    content: text.split('\n').map((line) => ({
      type: 'paragraph',
      content: line ? [{ type: 'text', text: line }] : [],
    })),
  };
}

export interface HighlightRange {
  /** Rune offsets into text, same [start, end) convention as a queel Slot
   *  and as TextSelection.start/end — not raw ProseMirror positions. */
  start: number;
  end: number;
  color: string;
}

/** Like docFromText, but wraps each given rune range in its own highlight
 *  mark — for restoring a text's existing slots as colored zones when it's
 *  reopened, instead of starting from a blank, uncolored document. Ranges
 *  must not overlap (queel never opens overlapping slots within a round). */
export function docFromTextWithHighlights(text: string, ranges: HighlightRange[]): unknown {
  const lines = text.split('\n');
  let cursor = 0;

  const paragraphs = lines.map((line) => {
    const lineRunes = Array.from(line);
    const lineStart = cursor;
    const lineEnd = lineStart + lineRunes.length;
    cursor = lineEnd + 1; // account for the '\n' separator between lines

    const overlapping = ranges.filter((r) => r.start < lineEnd && r.end > lineStart);
    const boundaries = new Set<number>([lineStart, lineEnd]);
    for (const r of overlapping) {
      boundaries.add(Math.max(r.start, lineStart));
      boundaries.add(Math.min(r.end, lineEnd));
    }
    const points = [...boundaries].sort((a, b) => a - b);

    const content: unknown[] = [];
    for (let i = 0; i < points.length - 1; i++) {
      const segStart = points[i];
      const segEnd = points[i + 1];
      if (segStart === segEnd) continue;
      const segText = lineRunes.slice(segStart - lineStart, segEnd - lineStart).join('');
      const range = overlapping.find((r) => segStart >= r.start && segEnd <= r.end);
      content.push(
        range
          ? { type: 'text', text: segText, marks: [{ type: 'highlight', attrs: { color: range.color } }] }
          : { type: 'text', text: segText },
      );
    }

    return { type: 'paragraph', content };
  });

  return { type: 'doc', content: paragraphs };
}

export interface TextSelection {
  from: number;
  to: number;
  /** Rune offset into getText()'s output — matches the [start, end) slot
   *  convention the queel API expects, not the raw ProseMirror position. */
  start: number;
  end: number;
  text: string;
}

@Component({
  selector: 'app-editor',
  template: `<div #editor class="h-full"></div>`,
  styles: [':host { display: block; height: 100%; }']
})
export class EditorComponent implements AfterViewInit {

  @ViewChild('editor')
  editor!: ElementRef;

  // Serialized doc (view.state.doc.toJSON()) to restore from — set this when
  // the component is recreated after being torn down (e.g. by an @if swap)
  // so its content and marks survive the round trip instead of coming back
  // blank.
  @Input() initialDoc: unknown = null;

  view!: EditorView;

  ngAfterViewInit() {
    const doc = this.initialDoc ? PMNode.fromJSON(schema, this.initialDoc) : undefined;

    this.view = new EditorView(
      this.editor.nativeElement,
      {
        state: EditorState.create({
          schema,
          doc
        })
      }
    );
  }

  getText(): string {
    return this.view.state.doc.textBetween(0, this.view.state.doc.content.size, '\n');
  }

  getDocJSON(): unknown {
    return this.view.state.doc.toJSON();
  }

  getSelection(): TextSelection | null {
    const { from, to, empty } = this.view.state.selection;
    if (empty) return null;

    const doc = this.view.state.doc;
    const text = doc.textBetween(from, to, '\n');
    // Same block separator as getText(), so `start` is exactly the rune
    // offset into that string where the selection begins — and `end` (start
    // + the selection's own rune count) lands one past its last character,
    // matching the [start, end) convention queel's Slot uses.
    const start = doc.textBetween(0, from, '\n').length;
    const end = start + Array.from(text).length;

    return { from, to, start, end, text };
  }

  highlightSelection(color: string = DEFAULT_HIGHLIGHT_COLOR): TextSelection | null {
    const selection = this.getSelection();
    if (!selection) return null;

    const { from, to } = selection;
    const markType = this.view.state.schema.marks['highlight'];
    const tr = this.view.state.tr.addMark(from, to, markType.create({ color }));
    this.view.dispatch(tr);
    this.view.focus();

    return selection;
  }
}
