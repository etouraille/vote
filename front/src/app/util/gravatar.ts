// Gravatar looks up an avatar by a hash of the account's email — SHA-256 is
// accepted alongside the legacy MD5 (https://docs.gravatar.com/api/avatars/images/),
// and SHA-256 is natively available via SubtleCrypto, so no hashing
// dependency is needed for something this small.
async function sha256Hex(input: string): Promise<string> {
  const bytes = new TextEncoder().encode(input);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

// d=identicon gives every account a distinct generated avatar instead of
// Gravatar's generic "mystery person" when they have no Gravatar profile.
export async function gravatarUrl(email: string, size = 32): Promise<string> {
  const hash = await sha256Hex(email.trim().toLowerCase());
  return `https://www.gravatar.com/avatar/${hash}?s=${size}&d=identicon`;
}
