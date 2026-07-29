/// One entry of api's GET /api/me/subscriptions — a followed text reduced
/// to what a list of titles needs. The content is deliberately absent
/// server-side too; opening an entry fetches the full text by id.
class SubscribedText {
  SubscribedText({required this.id, required this.title});

  factory SubscribedText.fromJson(Map<String, dynamic> json) {
    return SubscribedText(id: json['id'] as String, title: json['title'] as String);
  }

  final String id;
  final String title;
}
