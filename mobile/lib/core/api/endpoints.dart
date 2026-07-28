/// api route paths, relative to Env.apiBaseUrl (see ApiClient) — kept here
/// so a path never needs retyping (or drifting) at each call site.
class Endpoints {
  Endpoints._();

  static const login = '/api/auth/login';

  static String search(String query) => '/api/texts/search?q=${Uri.encodeQueryComponent(query)}';

  static String text(String id) => '/api/texts/$id';
}
