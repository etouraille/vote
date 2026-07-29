import 'dart:convert';

import 'package:http/http.dart' as http;

import '../../app/config/env.dart';
import '../network/exceptions.dart';
import '../storage/secure_storage.dart';

/// Talks to api's `/api/...` routes over Env.apiBaseUrl. Attaches the
/// stored session token (see SecureStorage) as a bearer header when one
/// exists, matching the front end's own auth interceptor — every route
/// this app calls (search, text detail) requires one; only login itself
/// doesn't.
class ApiClient {
  ApiClient._();

  static Future<dynamic> get(String path) async {
    final response = await http.get(Uri.parse('${Env.apiBaseUrl}$path'), headers: await _headers());
    return _decode(response);
  }

  static Future<dynamic> post(String path, Map<String, dynamic> body) async {
    final response = await http.post(
      Uri.parse('${Env.apiBaseUrl}$path'),
      headers: await _headers(),
      body: jsonEncode(body),
    );
    return _decode(response);
  }

  /// Tells the api which front end is calling. Only POST /api/auth/google
  /// acts on it today — web and mobile sign in through separate Google
  /// OAuth clients, so the api needs to know whose `aud` claim to expect
  /// (see googleAudience in api/auth.go) — but it's sent on every request
  /// so identifying the app never depends on which route is hit.
  static const _clientHeaderName = 'X-Queel-Client';
  static const _clientHeaderValue = 'mobile';

  static Future<Map<String, String>> _headers() async {
    final headers = {
      'Content-Type': 'application/json',
      _clientHeaderName: _clientHeaderValue,
    };
    final token = await SecureStorage.readValidToken();
    if (token != null) {
      headers['Authorization'] = 'Bearer $token';
    }
    return headers;
  }

  static dynamic _decode(http.Response response) {
    final body = response.body.isEmpty ? null : jsonDecode(utf8.decode(response.bodyBytes));
    if (response.statusCode < 200 || response.statusCode >= 300) {
      final message = body is Map && body['error'] is String ? body['error'] as String : 'Erreur serveur';
      throw ApiException(response.statusCode, message);
    }
    return body;
  }
}
