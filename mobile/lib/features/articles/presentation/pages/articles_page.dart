import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import 'text_detail_page.dart';
import '../../data/datasources/article_api.dart';
import '../../data/models/article.dart';
import '../../data/models/tag_count.dart';

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

  /// The labels in use, which the field completes on. Kept whole rather
  /// than listed on screen: a filter that also prints every label available
  /// says twice what completion already says once, and grows unreadable at
  /// the first few dozen.
  List<TagCount> _tags = const [];
  bool _filterOpen = false;

  /// What is being crossed, as one raw line. Sent to the api unsplit:
  /// reading a line into labels is one rule and it lives there — splitting
  /// it here would let "loi vote" mean two labels on this screen and one
  /// everywhere else.
  final _typed = TextEditingController();
  final _typedFocus = FocusNode();

  bool _loadingMore = false;
  bool _hasMore = true;

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    _load();
    _loadTags();
  }

  /// Silent on failure: losing the labels costs the filter, never the list
  /// they would have narrowed.
  Future<void> _loadTags() async {
    try {
      final tags = await ArticleApi.tags();
      if (mounted) setState(() => _tags = tags);
    } catch (_) {
      if (mounted) setState(() => _tags = const []);
    }
  }

  void _clearTags() {
    if (_typed.text.isEmpty) return;
    setState(() {
      _typed.clear();
      _articles = null;
    });
    _load();
  }

  /// The line as one value the api reads into labels of its own.
  List<String> _activeTags() => [if (_typed.text.trim().isNotEmpty) _typed.text.trim()];

  void _applyFilter() {
    setState(() => _articles = null);
    _load();
  }

  /// Splits the line the way the eye does, for the completion only — which
  /// word is being typed, and which are already there. Never for the query:
  /// that goes whole to the api, so this approximation cannot make the
  /// filter disagree with the server about what was asked.
  /// Whether the line is ready for a new word — empty, or closed by a
  /// separator. Tells "the reader is still typing this label" apart from
  /// "they have finished it".
  static bool _endsOnSeparator(String line) =>
      line.isEmpty || RegExp(r'[#,;\s]$').hasMatch(line);

  static List<String> _words(String line) =>
      line.split(RegExp(r'[#,;\s]+')).where((word) => word.isNotEmpty).toList();

  /// Accents folded away, so typing "ecolo" finds "écologie".
  ///
  /// Labels keep their accents — they are what gets stored and displayed —
  /// and only the comparison drops them. Nobody reaches for the accented
  /// key while typing a filter, and a completion that demands it is a
  /// completion that never fires.
  static String _fold(String value) {
    const accented = 'àáâäãåçèéêëìíîïñòóôöõùúûüýÿœæ';
    const plain = 'aaaaaaceeeeiiiinooooouuuuyyoa';

    final folded = StringBuffer();
    for (final rune in value.toLowerCase().runes) {
      final index = accented.indexOf(String.fromCharCode(rune));
      folded.write(index == -1 ? String.fromCharCode(rune) : plain[index]);
    }
    return folded.toString();
  }

  /// Labels matching what is being typed, minus those already on the line:
  /// suggesting a label a second time offers a narrowing that would change
  /// nothing.
  ///
  /// Matched anywhere in the label rather than only at its start — someone
  /// looking for "loi-de-finances" is as likely to type "finances" — but
  /// those starting with what was typed come first, since that is what a
  /// reader expects to see at the top.
  Iterable<String> _suggestions(String line) {
    final words = _words(line.toLowerCase());
    final current = _endsOnSeparator(line) ? '' : (words.isEmpty ? '' : words.last);
    final already = words.take(words.length - (current.isEmpty ? 0 : 1)).toSet();
    final typed = _fold(current);

    final matches = <({String tag, int rank})>[];
    for (var i = 0; i < _tags.length; i++) {
      final tag = _tags[i].tag;
      if (already.contains(tag)) continue;
      if (typed.isNotEmpty && !_fold(tag).contains(typed)) continue;
      matches.add((tag: tag, rank: i));
    }

    // The api's own position is part of the key, not left to the sort:
    // List.sort is not stable in Dart, so equal-relevance labels would
    // otherwise come back in an order that could change between keystrokes.
    matches.sort((a, b) {
      final aStarts = _fold(a.tag).startsWith(typed);
      final bStarts = _fold(b.tag).startsWith(typed);
      if (aStarts != bStarts) return aStarts ? -1 : 1;
      return a.rank.compareTo(b.rank);
    });
    return matches.take(8).map((match) => match.tag);
  }

  int? _countFor(String tag) {
    for (final known in _tags) {
      if (known.tag == tag) return known.count;
    }
    return null;
  }

  /// Replaces the word being typed with the label chosen, and leaves a
  /// space so the next one can follow without touching the keyboard.
  void _completeWith(String tag) {
    final words = _words(_typed.text);
    if (!_endsOnSeparator(_typed.text) && words.isNotEmpty) words.removeLast();

    _typed.text = '${[...words, tag].join(' ')} ';
    _typed.selection = TextSelection.collapsed(offset: _typed.text.length);
    _applyFilter();
  }

  @override
  void dispose() {
    _typedFocus.dispose();
    _typed.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final articles = await ArticleApi.recent(limit: _pageSize, tags: _activeTags());
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
      final more = await ArticleApi.recent(
        limit: _pageSize,
        offset: _articles!.length,
        tags: _activeTags(),
      );
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

  /// The field to cross labels in, shown above the list rather than on a
  /// screen of its own: choosing them and seeing what they leave is one
  /// act.
  Widget _filterPanel() {
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    _activeTags().isEmpty
                        ? 'Tapez une ou plusieurs étiquettes'
                        : 'Articles portant toutes ces étiquettes',
                    style: Theme.of(context).textTheme.labelMedium,
                  ),
                ),
                if (_activeTags().isNotEmpty)
                  TextButton(onPressed: _clearTags, child: const Text('Tout enlever')),
              ],
            ),
            RawAutocomplete<String>(
              textEditingController: _typed,
              focusNode: _typedFocus,
              optionsBuilder: (value) => _suggestions(value.text),
              onSelected: _completeWith,
              fieldViewBuilder: (context, controller, focusNode, onSubmitted) {
                return TextField(
                  controller: controller,
                  focusNode: focusNode,
                  textInputAction: TextInputAction.search,
                  onSubmitted: (_) => _applyFilter(),
                  decoration: const InputDecoration(
                    isDense: true,
                    border: OutlineInputBorder(),
                    hintText: 'loi #vote — dièse facultatif',
                  ),
                );
              },
              optionsViewBuilder: (context, onSelected, options) {
                return Align(
                  alignment: Alignment.topLeft,
                  child: Material(
                    elevation: 4,
                    child: ConstrainedBox(
                      // Bounded, or a long list of labels would cover the
                      // articles the filter exists to reveal.
                      constraints: const BoxConstraints(maxHeight: 220, maxWidth: 320),
                      child: ListView(
                        shrinkWrap: true,
                        padding: EdgeInsets.zero,
                        children: [
                          for (final option in options)
                            ListTile(
                              dense: true,
                              title: Text('#$option'),
                              trailing: Text(
                                // Empty rather than throwing if the label
                                // list has been reloaded since the options
                                // were built: a missing count is worth far
                                // less than a crash.
                                '${_countFor(option) ?? ''}',
                                style: Theme.of(context).textTheme.labelSmall,
                              ),
                              onTap: () => onSelected(option),
                            ),
                        ],
                      ),
                    ),
                  ),
                );
              },
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: QueelAppBar(
        title: _activeTags().isEmpty ? 'Articles' : 'Articles filtrés',
        actions: [
          if (_tags.isNotEmpty)
            IconButton(
              onPressed: () => setState(() => _filterOpen = !_filterOpen),
              icon: Icon(_activeTags().isEmpty ? Icons.filter_list : Icons.filter_list_off),
              tooltip: 'Filtrer par étiquettes',
            ),
        ],
      ),
      body: Column(
        children: [
          if (_filterOpen) _filterPanel(),
          Expanded(child: _list()),
        ],
      ),
    );
  }

  Widget _list() {
    return switch ((_articles, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final articles?, _) when articles.isEmpty => RefreshIndicator(
            onRefresh: _load,
            // A ListView rather than a bare Center: pull-to-refresh needs
            // something scrollable under it.
            child: ListView(
              children: [
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 64),
                  child: Text(
                    _activeTags().isEmpty
                        ? 'Aucun article pour le moment.'
                        : 'Aucun article ne porte à la fois ${_activeTags().map((tag) => '#$tag').join(', ')}.',
                    textAlign: TextAlign.center,
                  ),
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
    };
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
