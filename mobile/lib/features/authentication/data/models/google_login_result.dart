import 'session.dart';

/// The two shapes api's POST /api/auth/google answers 200 with (see
/// googleLoginHandler): an ordinary session, or `{"needsPseudo": true}`
/// when this Google account has no user yet and the caller has to ask for
/// a pseudo and retry with the *same* ID token plus that pseudo.
///
/// Modelled as a sealed hierarchy rather than a Session with nullable
/// fields so the "needs a pseudo" branch can't be forgotten at the call
/// site — a switch over it has to handle both.
sealed class GoogleLoginResult {
  const GoogleLoginResult();
}

class GoogleLoginSuccess extends GoogleLoginResult {
  const GoogleLoginSuccess(this.session);

  final Session session;
}

class GoogleLoginNeedsPseudo extends GoogleLoginResult {
  const GoogleLoginNeedsPseudo();
}
