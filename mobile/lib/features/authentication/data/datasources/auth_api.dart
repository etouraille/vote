import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/session.dart';

class AuthApi {
  AuthApi._();

  static Future<Session> login(String email, String password) async {
    final json = await ApiClient.post(Endpoints.login, {'email': email, 'password': password});
    return Session.fromJson(json as Map<String, dynamic>);
  }
}
