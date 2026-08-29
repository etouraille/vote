import 'package:flutter/material.dart';

import '../../../../app/theme/colors.dart';
import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../../articles/presentation/pages/text_detail_page.dart';
import '../../../vote/presentation/pages/vote_page.dart';
import '../../data/datasources/subscription_api.dart';
import '../../data/models/subscribed_text.dart';

/// The texts the signed-in user follows, listed by title. Reached from the
/// overflow menu on any screen; tapping an entry opens the same
/// TextDetailPage the article list does.
class SubscriptionsPage extends StatefulWidget {
  const SubscriptionsPage({super.key});

  @override
  State<SubscriptionsPage> createState() => _SubscriptionsPageState();
}

class _SubscriptionsPageState extends State<SubscriptionsPage> {
  List<SubscribedText>? _texts;
  String? _error;

  /// Which one is being left, so its row can say so and not be tapped
  /// twice. One at a time is enough: leaving is a deliberate act.
  String? _leaving;

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

  /// Leaves a text, and drops it from the list.
  ///
  /// Removed rather than greyed out: this list is what you follow, and a
  /// text you have just left does not belong on it. Confirmed first — the
  /// row is small and the tap is next to the one that opens the text.
  Future<void> _unsubscribe(SubscribedText text) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('Ne plus suivre « ${text.title} » ?'),
        content: const Text(
          'Vous ne serez plus prévenu de ses modifications. Vous pourrez le suivre à nouveau depuis sa page.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(false), child: const Text('Annuler')),
          TextButton(onPressed: () => Navigator.of(context).pop(true), child: const Text('Ne plus suivre')),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _leaving = text.id);
    try {
      await SubscriptionApi.unsubscribe(text.id);
      if (mounted) setState(() => _texts = _texts?.where((item) => item.id != text.id).toList());
    } on ApiException catch (e) {
      _showMessage(e.message);
    } catch (_) {
      _showMessage('Désabonnement impossible.');
    } finally {
      if (mounted) setState(() => _leaving = null);
    }
  }

  void _showMessage(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  void _openText(SubscribedText text) {
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => TextDetailPage(textId: text.id)),
    );
  }

  void _openVote(SubscribedText text) {
    Navigator.of(context).push(
      MaterialPageRoute(builder: (_) => VotePage(textId: text.id)),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: const QueelAppBar(title: 'Mes abonnements'),
      body: switch ((_texts, _error)) {
        (null, null) => const Center(child: CircularProgressIndicator()),
        (null, final error?) => Center(
            child: Text(error, style: TextStyle(color: AppColors.removedInk)),
          ),
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
                // The row still opens the text; the two buttons are their
                // own targets so that reaching the round, or leaving,
                // never means opening the text first.
                trailing: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    TextButton(onPressed: () => _openVote(text), child: const Text('Voter')),
                    IconButton(
                      onPressed: _leaving == text.id ? null : () => _unsubscribe(text),
                      icon: const Icon(Icons.notifications_off_outlined),
                      tooltip: 'Ne plus suivre',
                    ),
                  ],
                ),
              );
            },
          ),
      },
    );
  }
}
