/// api route paths, relative to Env.apiBaseUrl (see ApiClient) — kept here
/// so a path never needs retyping (or drifting) at each call site.
class Endpoints {
  Endpoints._();

  static const login = '/api/auth/login';

  /// "Sign in with Google" — takes a Google ID token instead of a password.
  /// Only served when api's own GOOGLE_CLIENT_ID is set (see main.go), so a
  /// backend without it configured answers 404 here.
  static const googleLogin = '/api/auth/google';

  static String search(String query) => '/api/texts/search?q=${Uri.encodeQueryComponent(query)}';

  static String text(String id) => '/api/texts/$id';

  /// Follow a text — the caller is taken from the bearer token, so nothing
  /// identifies the user in the path or body.
  static String subscribe(String id) => '/api/texts/$id/subscribe';

  /// The texts the caller follows, id and title only.
  static const subscriptions = '/api/me/subscriptions';
}
