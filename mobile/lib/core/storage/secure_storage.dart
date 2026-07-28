import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// The session JWT and its expiry, in the platform's secure storage
/// (Keychain/Keystore) rather than SharedPreferences — same reasoning the
/// front end's own token storage follows: nothing else on the device
/// should be able to read this out of a plain file.
class SecureStorage {
  SecureStorage._();

  static const _storage = FlutterSecureStorage();
  static const _tokenKey = 'queel.auth.token';
  static const _expiresAtKey = 'queel.auth.expiresAt';

  static Future<void> writeSession(String token, DateTime expiresAt) async {
    await _storage.write(key: _tokenKey, value: token);
    await _storage.write(key: _expiresAtKey, value: expiresAt.toIso8601String());
  }

  /// The stored token, or null if there isn't one or it's already expired
  /// (in which case both entries are cleared, so a caller never has to
  /// separately check expiry itself).
  static Future<String?> readValidToken() async {
    final token = await _storage.read(key: _tokenKey);
    final expiresAtRaw = await _storage.read(key: _expiresAtKey);
    if (token == null || expiresAtRaw == null) return null;

    final expiresAt = DateTime.tryParse(expiresAtRaw);
    if (expiresAt == null || expiresAt.isBefore(DateTime.now())) {
      await clear();
      return null;
    }
    return token;
  }

  static Future<void> clear() async {
    await _storage.delete(key: _tokenKey);
    await _storage.delete(key: _expiresAtKey);
  }
}
