import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../data/datasources/search_api.dart';
import '../../data/models/search_result.dart';
import 'text_detail_page.dart';

/// The app's landing page once signed in — nothing but a search field and,
/// once a search runs, the matching titles below it. Tapping one pushes
/// TextDetailPage on top to show that text's content.
class SearchPage extends StatefulWidget {
  const SearchPage({super.key});

  @override
  State<SearchPage> createState() => _SearchPageState();
}

class _SearchPageState extends State<SearchPage> {
  final _queryController = TextEditingController();
  List<SearchResult>? _results;
  bool _searching = false;
  String? _error;

  @override
  void dispose() {
    _queryController.dispose();
    super.dispose();
  }

  Future<void> _search() async {
    final query = _queryController.text.trim();
    if (query.isEmpty || _searching) return;

    setState(() {
      _searching = true;
      _error = null;
    });
    try {
      final results = await SearchApi.search(query);
      if (mounted) setState(() => _results = results);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Recherche impossible.');
    } finally {
      if (mounted) setState(() => _searching = false);
    }
  }

  void _openText(SearchResult result) {
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => TextDetailPage(textId: result.textId)),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: const QueelAppBar(title: 'Queel'),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            TextField(
              controller: _queryController,
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                labelText: 'Rechercher un texte',
                suffixIcon: IconButton(icon: const Icon(Icons.search), onPressed: _search),
              ),
              onSubmitted: (_) => _search(),
            ),
            const SizedBox(height: 16),
            if (_searching) const CircularProgressIndicator(),
            if (_error != null) Text(_error!, style: const TextStyle(color: Colors.red)),
            if (_results != null)
              Expanded(
                child: _results!.isEmpty
                    ? const Text('Aucun résultat.')
                    : ListView.builder(
                        itemCount: _results!.length,
                        itemBuilder: (context, index) {
                          final result = _results![index];
                          return ListTile(
                            title: Text(result.title),
                            onTap: () => _openText(result),
                          );
                        },
                      ),
              ),
          ],
        ),
      ),
    );
  }
}
