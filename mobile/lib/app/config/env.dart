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
