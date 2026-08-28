/// api's GET /api/texts/{id} response — just the fields the detail view
/// shows; queel.Text carries a few more (finalized, createdBy, ...) this
/// view has no use for yet.
class TextDetail {
  TextDetail({required this.id, required this.title, required this.content});

  factory TextDetail.fromJson(Map<String, dynamic> json) {
    return TextDetail(
      id: json['id'] as String,
      title: json['title'] as String,
      content: json['content'] as String,
    );
  }

  final String id;
  final String title;
  final String content;
}
