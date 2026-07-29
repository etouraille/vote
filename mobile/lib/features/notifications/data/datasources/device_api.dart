import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';

class DeviceApi {
  DeviceApi._();

  /// Tells the api this device can be reached at [token].
  ///
  /// Called on every launch and again whenever FCM rotates the token: the
  /// app has no way of knowing whether the server already has it, and the
  /// route is idempotent precisely so it doesn't need to.
  static Future<void> register(String token, {String platform = 'android'}) async {
    await ApiClient.post(Endpoints.devices, {'token': token, 'platform': platform});
  }
}
