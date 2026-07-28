/// api's POST /api/auth/login response — only the two fields this app
/// actually needs (the session token and its expiry); root/permissions
/// aren't used anywhere here yet.
class Session {
  Session({required this.token, required this.expiresAt});

  factory Session.fromJson(Map<String, dynamic> json) {
    return Session(
      token: json['token'] as String,
      expiresAt: DateTime.parse(json['expiresAt'] as String),
    );
  }

  final String token;
  final DateTime expiresAt;
}
