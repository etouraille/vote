/// One entry from api's GET /api/texts/search response.
class SearchResult {
  SearchResult({required this.textId, required this.title});

  factory SearchResult.fromJson(Map<String, dynamic> json) {
    return SearchResult(
      textId: json['textId'] as String,
      title: json['title'] as String,
    );
  }

  final String textId;
  final String title;
}
