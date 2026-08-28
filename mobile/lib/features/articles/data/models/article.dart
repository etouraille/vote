/// One entry of GET /api/texts — the listing the app opens on.
///
/// Only ever the current version of a text: the api leaves out the ones a
/// closed round has already forked, so reading down this list is reading
/// each article as its latest round settled it.
class Article {
  Article({
    required this.id,
    required this.title,
    required this.tags,
    required this.subscribed,
    required this.createdAt,
  });

  factory Article.fromJson(Map<String, dynamic> json) {
    return Article(
      id: json['id'] as String,
      title: json['title'] as String,
      // Absent on an older api, empty on a text nobody labelled — the same
      // thing as far as this list is concerned.
      tags: ((json['tags'] as List?) ?? const []).map((tag) => tag as String).toList(),
      subscribed: json['subscribed'] as bool? ?? false,
      createdAt: DateTime.parse(json['createdAt'] as String).toLocal(),
    );
  }

  final String id;
  final String title;
  final List<String> tags;

  /// Whether the reader follows it, so the list can say so without asking
  /// again per row.
  final bool subscribed;

  final DateTime createdAt;
}
