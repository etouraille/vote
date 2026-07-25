import { Service } from '@angular/core';

const STORAGE_KEY = 'vote.editor.lastText';

export interface LastText {
  id: string;
  title: string;
}

// Remembers the most recently saved text (id + title) across reloads —
// a single slot, overwritten on every save, not a growing history.
@Service()
export class LastTextStorage {
  read(): LastText | null {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    try {
      return JSON.parse(raw) as LastText;
    } catch {
      return null;
    }
  }

  write(text: LastText): void {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(text));
  }

  clear(): void {
    localStorage.removeItem(STORAGE_KEY);
  }
}
