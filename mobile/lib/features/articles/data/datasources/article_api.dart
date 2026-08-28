import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/article.dart';
import '../models/text_detail.dart';

class ArticleApi {
  ArticleApi._();

  /// The most recent articles, newest first — the order the api already
  /// returns them in, so nothing here re-sorts them.
  ///
  /// [offset] pages through: each call asks for the next [limit] past what
  /// is already on screen.
  /// One article in full, for its own page.
  static Future<TextDetail> text(String id) async {
    final json = await ApiClient.get(Endpoints.text(id));
    return TextDetail.fromJson(json as Map<String, dynamic>);
  }

  static Future<List<Article>> recent({int limit = 20, int offset = 0}) async {
    final json = await ApiClient.get(Endpoints.recentTexts(limit, offset));
    return (json as List).map((item) => Article.fromJson(item as Map<String, dynamic>)).toList();
  }
}
