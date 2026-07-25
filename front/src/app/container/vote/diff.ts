export interface DiffToken {
  type: 'equal' | 'removed' | 'added';
  text: string;
}

// Splits on whitespace, keeping the whitespace itself as tokens (in the
// capturing group), so re-joining diffed tokens reproduces exact spacing.
function tokenize(text: string): string[] {
  return text.split(/(\s+)/).filter((t) => t.length > 0);
}

// Word-level diff via a classic LCS (longest common subsequence) walk — no
// external dependency, fine at the size of a single slot's text (at most a
// few hundred tokens, so the O(n*m) table is trivial).
export function diffWords(oldText: string, newText: string): DiffToken[] {
  const a = tokenize(oldText);
  const b = tokenize(newText);
  const n = a.length;
  const m = b.length;

  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }

  const raw: DiffToken[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      raw.push({ type: 'equal', text: a[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      raw.push({ type: 'removed', text: a[i] });
      i++;
    } else {
      raw.push({ type: 'added', text: b[j] });
      j++;
    }
  }
  while (i < n) raw.push({ type: 'removed', text: a[i++] });
  while (j < m) raw.push({ type: 'added', text: b[j++] });

  // Merge adjacent same-type tokens so the template renders one <span> per
  // run of changes instead of one per word.
  const merged: DiffToken[] = [];
  for (const token of raw) {
    const last = merged[merged.length - 1];
    if (last && last.type === token.type) {
      last.text += token.text;
    } else {
      merged.push({ ...token });
    }
  }
  return merged;
}
