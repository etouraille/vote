import 'package:flutter/material.dart';

import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../../search/presentation/pages/text_detail_page.dart';
import '../../data/datasources/subscription_api.dart';
import '../../data/models/subscribed_text.dart';

/// The texts the signed-in user follows, listed by title. Reached from the
/// search page's overflow menu; tapping an entry opens the same
/// TextDetailPage a search result does.
class SubscriptionsPage extends StatefulWidget {
  const SubscriptionsPage({super.key});

  @override
  State<SubscriptionsPage> createState() => _SubscriptionsPageState();
}

class _SubscriptionsPageState extends State<SubscriptionsPage> {
  List<SubscribedText>? _texts;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final texts = await SubscriptionApi.list();
      if (mounted) setState(() => _texts = texts);
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    } catch (_) {
      if (mounted) setState(() => _error = 'Chargement impossible.');
    }
  }

  void _openText(SubscribedText text) {
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => TextDetailPage(textId: text.id)),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: const QueelAppBar(title: 'Mes abonnements'),
      body: switch ((_texts, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(child: Text(error, style: const TextStyle(color: Colors.red))),
        (final texts?, _) when texts.isEmpty => const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text(
                "Vous ne suivez aucun texte pour l'instant.",
                textAlign: TextAlign.center,
              ),
            ),
          ),
        (final texts?, _) => ListView.separated(
            itemCount: texts.length,
            separatorBuilder: (_, _) => const Divider(height: 1),
            itemBuilder: (_, index) {
              final text = texts[index];
              return ListTile(
                title: Text(text.title),
                onTap: () => _openText(text),
              );
            },
          ),
      },
    );
  }
}
