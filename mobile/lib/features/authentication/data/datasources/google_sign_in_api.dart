import 'package:google_sign_in/google_sign_in.dart';

import '../../../../app/config/env.dart';

/// Thin wrapper over google_sign_in, kept here so the login page deals in
/// "give me an ID token or null" instead of the plugin's lifecycle.
class GoogleSignInApi {
  GoogleSignInApi._();

  /// initialize() must run exactly once before any authenticate() call, and
  /// the plugin gives no "already initialized" query — so the Future is
  /// cached and awaited by every caller rather than re-run.
  static Future<void>? _initialization;

  static Future<void> _ensureInitialized() {
    return _initialization ??= GoogleSignIn.instance.initialize(
      serverClientId: Env.googleServerClientId,
    );
  }

  /// Whether this platform can drive its own sign-in UI. False on web,
  /// where google_sign_in requires a Google-rendered button instead of an
  /// app-drawn one — the login page hides its Google button in that case
  /// rather than offering something that would throw UnsupportedError.
  ///
  /// Also false wherever no platform implementation is registered at all:
  /// the platform interface's placeholder throws UnimplementedError rather
  /// than answering, which is what a plain widget test gets. Treating that
  /// as "unsupported" keeps the page buildable off-device.
  static bool get isSupported {
    try {
      return GoogleSignIn.instance.supportsAuthenticate();
    } on UnimplementedError {
      return false;
    }
  }

  /// Runs the interactive Google sign-in and returns the resulting ID token
  /// for the backend to verify.
  ///
  /// Returns null when the user backs out of the account picker — an
  /// ordinary outcome, not a failure worth showing an error for. Every
  /// other GoogleSignInException propagates.
  static Future<String?> signInIdToken() async {
    await _ensureInitialized();

    final GoogleSignInAccount account;
    try {
      account = await GoogleSignIn.instance.authenticate();
    } on GoogleSignInException catch (e) {
      if (e.code == GoogleSignInExceptionCode.canceled) return null;
      rethrow;
    }

    return account.authentication.idToken;
  }

  /// Clears the plugin's own cached account so the next sign-in shows the
  /// picker again. The backend session is separate — see
  /// SecureStorage.
  static Future<void> signOut() async {
    await _ensureInitialized();
    await GoogleSignIn.instance.signOut();
  }
}
