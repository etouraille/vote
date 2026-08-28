/// api's GET /api/texts/{id} response — just the fields the article page
/// shows; queel.Text carries a few more (finalized, createdBy, ...) this
/// view has no use for yet.
class TextDetail {
  TextDetail({required this.id, required this.title, required this.content, required this.tags});

  factory TextDetail.fromJson(Map<String, dynamic> json) {
    return TextDetail(
      id: json['id'] as String,
      title: json['title'] as String,
      content: json['content'] as String,
      // Absent on an older api, empty on a text nobody labelled — the same
      // thing as far as this page is concerned.
      tags: ((json['tags'] as List?) ?? const []).map((tag) => tag as String).toList(),
    );
  }

  final String id;
  final String title;
  final String content;

  /// The labels the author gave it, already parsed and normalised by the
  /// api — displayed with their hash, which is not stored.
  final List<String> tags;
}
