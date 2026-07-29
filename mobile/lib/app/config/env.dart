import 'package:flutter_dotenv/flutter_dotenv.dart';

/// Reads lib/config/.env — the raw dotenv file stays there (see that
/// directory), matching pubspec.yaml's asset path; this just centralizes
/// loading it and reading values out of it.
class Env {
  Env._();

  static Future<void> load() => dotenv.load(fileName: 'lib/config/.env');

  static String get apiBaseUrl => dotenv.env['API_BASE_URL'] ?? '';

  /// Web OAuth client ID handed to GoogleSignIn.initialize — see the .env
  /// entry itself for what it has to match and why.
  static String get googleServerClientId => _required('GOOGLE_SERVER_CLIENT_ID');

  /// Firebase Cloud Messaging settings — the same values a
  /// google-services.json carries. See the .env file itself for how to
  /// obtain them and for the full Android setup.
  static String get firebaseApiKey => dotenv.env['FIREBASE_API_KEY'] ?? '';
  static String get firebaseAppId => dotenv.env['FIREBASE_APP_ID'] ?? '';
  static String get firebaseMessagingSenderId => dotenv.env['FIREBASE_MESSAGING_SENDER_ID'] ?? '';
  static String get firebaseProjectId => dotenv.env['FIREBASE_PROJECT_ID'] ?? '';

  /// Whether push notifications can be set up at all. Deliberately a
  /// silent "no" rather than a throw like _required below: an app without
  /// notifications configured must still run, it just stays quiet.
  static bool get pushConfigured =>
      firebaseApiKey.isNotEmpty &&
      firebaseAppId.isNotEmpty &&
      firebaseMessagingSenderId.isNotEmpty &&
      firebaseProjectId.isNotEmpty;

  /// Reads a key that has no sensible fallback, throwing rather than
  /// defaulting to '' the way apiBaseUrl does.
  ///
  /// An empty client ID doesn't fail where it's set: GoogleSignIn.initialize
  /// accepts it and the sign-in only breaks later, somewhere inside Google's
  /// SDK, with nothing pointing back at the missing key. Failing here names
  /// it instead.
  static String _required(String key) {
    final value = dotenv.env[key];
    if (value == null || value.isEmpty) {
      throw StateError('lib/config/.env: $key is missing');
    }
    return value;
  }
}
