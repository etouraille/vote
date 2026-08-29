import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/google_login_result.dart';
import '../models/session.dart';

class AuthApi {
  AuthApi._();

  static Future<Session> login(String email, String password) async {
    final json = await ApiClient.post(Endpoints.login, {'email': email, 'password': password});
    return Session.fromJson(json as Map<String, dynamic>);
  }

  /// Creates an account and asks the api to send its confirmation email.
  ///
  /// Nothing comes back to hold on to: there is no session yet, and there
  /// cannot be one until the emailed link is followed. [pseudo] is
  /// optional — the api takes an empty one and the account simply shows
  /// its email until one is set.
  static Future<void> register(String email, String pseudo, String password) async {
    await ApiClient.post(Endpoints.register, {
      'email': email,
      'pseudo': pseudo,
      'password': password,
    });
  }

  /// Exchanges a Google ID token (see GoogleSignInApi) for a session.
  ///
  /// [pseudo] is only for the first sign-in of a Google account the
  /// backend has never seen: leaving it null the first time is the normal
  /// flow, and getting GoogleLoginNeedsPseudo back is the signal to prompt
  /// and call this again with the same [idToken] plus a pseudo.
  static Future<GoogleLoginResult> googleLogin(String idToken, {String? pseudo}) async {
    final json = await ApiClient.post(Endpoints.googleLogin, {
      'idToken': idToken,
      'pseudo': ?pseudo,
    });
    final body = json as Map<String, dynamic>;
    if (body['needsPseudo'] == true) {
      return const GoogleLoginNeedsPseudo();
    }
    return GoogleLoginSuccess(Session.fromJson(body));
  }
}
