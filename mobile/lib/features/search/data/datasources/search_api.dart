import '../../../../core/api/api_client.dart';
import '../../../../core/api/endpoints.dart';
import '../models/search_result.dart';
import '../models/text_detail.dart';

class SearchApi {
  SearchApi._();

  static Future<List<SearchResult>> search(String query) async {
    final json = await ApiClient.get(Endpoints.search(query));
    return (json as List).map((item) => SearchResult.fromJson(item as Map<String, dynamic>)).toList();
  }

  static Future<TextDetail> text(String id) async {
    final json = await ApiClient.get(Endpoints.text(id));
    return TextDetail.fromJson(json as Map<String, dynamic>);
  }
}
