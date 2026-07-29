import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../../subscriptions/data/datasources/subscription_api.dart';
import '../../data/datasources/search_api.dart';
import '../../data/models/text_detail.dart';

/// Pushed on top of the search results (a normal Navigator push already
/// layers it over the previous screen) when a title is tapped — shows that
/// text's full content, and lets the reader follow it.
class TextDetailPage extends StatefulWidget {
  const TextDetailPage({super.key, required this.textId});

  final String textId;

  @override
  State<TextDetailPage> createState() => _TextDetailPageState();
}

class _TextDetailPageState extends State<TextDetailPage> {
  TextDetail? _text;
  String? _error;

  /// Null until the subscription list has been read, and it stays null if
  /// that read fails. Unknown is treated as "not subscribed" for display:
  /// offering the action costs at worst one redundant tap (subscribing
  /// twice is a no-op server-side), whereas hiding it removes the feature
  /// outright over a request that says nothing about this text.
  bool? _subscribed;
  bool _subscribing = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    // Both in flight at once: the subscription state is only needed to
    // label the button, so it must not delay showing the text.
    final textRequest = SearchApi.text(widget.textId);
    final subscriptionsRequest = SubscriptionApi.list();

    try {
      final text = await textRequest;
      if (mounted) setState(() => _text = text);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Chargement impossible.');
    }

    try {
      final subscriptions = await subscriptionsRequest;
      final followed = subscriptions.any((s) => s.id == widget.textId);
      if (mounted) setState(() => _subscribed = followed);
    } catch (_) {
      // Leaving _subscribed null hides the button entirely: failing to
      // read the list says nothing about whether this text is followed,
      // and the whole page shouldn't error over it.
    }
  }

  Future<void> _subscribe() async {
    if (_subscribing) return;
    setState(() => _subscribing = true);

    try {
      await SubscriptionApi.subscribe(widget.textId);
      if (mounted) setState(() => _subscribed = true);
    } on ApiException catch (e) {
      _showMessage(e.message);
    } catch (_) {
      _showMessage('Abonnement impossible.');
    } finally {
      if (mounted) setState(() => _subscribing = false);
    }
  }

  void _showMessage(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: QueelAppBar(title: _text?.title ?? ''),
      body: switch ((_text, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final text?, _) => SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (_subscribed ?? false)
                  const Row(
                    children: [
                      Icon(Icons.check, size: 18),
                      SizedBox(width: 6),
                      Text('Abonné'),
                    ],
                  )
                else
                  FilledButton(
                    onPressed: _subscribing ? null : _subscribe,
                    child: Text(_subscribing ? 'Abonnement…' : "S'abonner"),
                  ),
                const SizedBox(height: 16),
                Text(text.content),
              ],
            ),
          ),
      },
    );
  }
}
