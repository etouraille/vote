import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../../search/presentation/pages/text_detail_page.dart';
import '../../data/datasources/article_api.dart';
import '../../data/models/article.dart';

/// One page of articles. The list scrolls, so this is a batch size rather
/// than a screenful.
const _pageSize = 20;

/// Where the app opens once signed in: every article, newest first.
///
/// Each row is a title and its labels, which is what someone scanning a
/// list decides on; the text itself is one tap away. Only current versions
/// appear — the api leaves out those a closed round has forked — so the
/// list is every article as its latest round settled it.
class ArticlesPage extends StatefulWidget {
  const ArticlesPage({super.key});

  @override
  State<ArticlesPage> createState() => _ArticlesPageState();
}

class _ArticlesPageState extends State<ArticlesPage> {
  final _scroll = ScrollController();

  List<Article>? _articles;
  String? _error;

  bool _loadingMore = false;
  bool _hasMore = true;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    _load();
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final articles = await ArticleApi.recent(limit: _pageSize);
      if (!mounted) return;
      setState(() {
        _articles = articles;
        _error = null;
        _hasMore = articles.length == _pageSize;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Chargement impossible.');
    }
  }

  void _onScroll() {
    if (_scroll.position.pixels < _scroll.position.maxScrollExtent - 200) return;
    _loadMore();
  }

  Future<void> _loadMore() async {
    // A page shorter than asked for is the end of the list; the guard on
    // _loadingMore stops rapid scroll events firing the same page twice.
    if (_loadingMore || !_hasMore || _articles == null) return;
    setState(() => _loadingMore = true);

    try {
      final more = await ArticleApi.recent(limit: _pageSize, offset: _articles!.length);
      if (!mounted) return;
      setState(() {
        _articles = [..._articles!, ...more];
        _hasMore = more.length == _pageSize;
      });
    } catch (_) {
      // Silent: the list already on screen stays usable, and scrolling
      // again retries.
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  Future<void> _open(Article article) async {
    await Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => TextDetailPage(textId: article.id)),
    );
    // Coming back from the article: following it there must show here too,
    // and the flag rides on the listing rather than on this page's state.
    if (mounted) await _load();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: const QueelAppBar(title: 'Articles'),
      body: switch ((_articles, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final articles?, _) when articles.isEmpty => RefreshIndicator(
            onRefresh: _load,
            // A ListView rather than a bare Center: pull-to-refresh needs
            // something scrollable under it.
            child: ListView(
              children: const [
                Padding(
                  padding: EdgeInsets.symmetric(horizontal: 24, vertical: 64),
                  child: Text('Aucun article pour le moment.', textAlign: TextAlign.center),
                ),
              ],
            ),
          ),
        (final articles?, _) => RefreshIndicator(
            onRefresh: _load,
            child: ListView.separated(
              controller: _scroll,
              itemCount: articles.length + (_loadingMore ? 1 : 0),
              separatorBuilder: (_, _) => const Divider(height: 1),
              itemBuilder: (_, index) {
                if (index == articles.length) {
                  return const Padding(
                    padding: EdgeInsets.all(16),
                    child: Center(child: CircularProgressIndicator()),
                  );
                }
                return _ArticleTile(article: articles[index], onTap: () => _open(articles[index]));
              },
            ),
          ),
      },
    );
  }
}

class _ArticleTile extends StatelessWidget {
  const _ArticleTile({required this.article, required this.onTap});

  final Article article;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListTile(
      onTap: onTap,
      title: Text(article.title, style: const TextStyle(fontWeight: FontWeight.w600)),
      subtitle: article.tags.isEmpty
          ? null
          : Padding(
              padding: const EdgeInsets.only(top: 6),
              child: Wrap(
                spacing: 6,
                runSpacing: 4,
                children: [
                  for (final tag in article.tags)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                      decoration: BoxDecoration(
                        color: theme.colorScheme.surfaceContainerHighest,
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text('#$tag', style: theme.textTheme.labelSmall),
                    ),
                ],
              ),
            ),
      // Following is a detail beside the title, not an action here: it is
      // decided on the article's own page, having read it.
      trailing: article.subscribed
          ? Icon(Icons.check_circle, size: 18, color: Colors.green.shade700)
          : const Icon(Icons.chevron_right),
    );
  }
}
