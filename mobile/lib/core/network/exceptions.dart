/// Thrown by [ApiClient] for any non-2xx response — statusCode and the
/// server's own error message (api's handlers always respond with
/// `{"error": "..."}` on failure) so callers can show it directly rather
/// than a generic "something went wrong".
class ApiException implements Exception {
  ApiException(this.statusCode, this.message);

  final int statusCode;
  final String message;

  @override
  String toString() => 'ApiException($statusCode): $message';
}
