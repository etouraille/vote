// Web OAuth client ID from Google Cloud Console (APIs & Services >
// Credentials > Create Credentials > OAuth client ID > "Web application") —
// public by design, used client-side to initialize Google Identity
// Services (see login.ts). Must match GOOGLE_CLIENT_ID in api/.env exactly:
// the backend verifies every incoming ID token's `aud` claim against that
// same value (see queel/rbac.VerifyGoogleIDToken), so a mismatch here
// rejects every Google sign-in attempt.
export const GOOGLE_CLIENT_ID = 'REPLACE_WITH_YOUR_GOOGLE_CLIENT_ID.apps.googleusercontent.com';
