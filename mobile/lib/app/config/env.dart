import 'package:flutter_dotenv/flutter_dotenv.dart';

/// Reads lib/config/.env — the raw dotenv file stays there (see that
/// directory), matching pubspec.yaml's asset path; this just centralizes
/// loading it and reading values out of it.
class Env {
  Env._();

  static Future<void> load() => dotenv.load(fileName: 'lib/config/.env');

  static String get apiBaseUrl => dotenv.env['API_BASE_URL'] ?? '';
}
