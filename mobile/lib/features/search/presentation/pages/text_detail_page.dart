import 'package:flutter/material.dart';

import '../../../../core/network/exceptions.dart';
import '../../data/datasources/search_api.dart';
import '../../data/models/text_detail.dart';

/// Pushed on top of the search results (a normal Navigator push already
/// layers it over the previous screen) when a title is tapped — shows that
/// text's full content.
class TextDetailPage extends StatefulWidget {
  const TextDetailPage({super.key, required this.textId});

  final String textId;

  @override
  State<TextDetailPage> createState() => _TextDetailPageState();
}

class _TextDetailPageState extends State<TextDetailPage> {
  TextDetail? _text;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final text = await SearchApi.text(widget.textId);
      if (mounted) setState(() => _text = text);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Chargement impossible.');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(_text?.title ?? '')),
      body: switch ((_text, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final text?, _) => SingleChildScrollView(
            padding: const EdgeInsets.all(16),
            child: Text(text.content),
          ),
      },
    );
  }
}
