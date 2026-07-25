// Last `count` words of text up to (not including) the rune offset `end` —
// same rune-offset convention as TextSelection.start/end (see app-editor).
export function lastWords(text: string, end: number, count: number): string {
  const before = Array.from(text).slice(0, end).join('');
  return before.split(/\s+/).filter(Boolean).slice(-count).join(' ');
}

// First `count` words of text starting at the rune offset `start`.
export function firstWords(text: string, start: number, count: number): string {
  const after = Array.from(text).slice(start).join('');
  return after.split(/\s+/).filter(Boolean).slice(0, count).join(' ');
}
