import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';

import '../../../../app/theme/colors.dart';
import '../../../../app/widgets/queel_app_bar.dart';
import '../../../../core/network/exceptions.dart';
import '../../data/datasources/vote_api.dart';
import '../../data/models/vote_models.dart';

/// One slot with everything the page needs to render and vote on it.
class _SlotGroup {
  _SlotGroup({
    required this.slot,
    required this.original,
    required this.seedFragmentId,
    required this.proposals,
  });

  final Slot slot;

  /// The wording this slot currently holds, sliced out of the text.
  final String original;

  /// The seed fragment's id — what "voter pour le texte original" votes
  /// for. Null only if queel somehow didn't seed the slot, which shouldn't
  /// happen; the original-vote button is disabled in that case rather than
  /// sending a request that can't succeed.
  final String? seedFragmentId;

  final List<Fragment> proposals;

  bool voting = false;
  String? votedFragmentId;
  String? error;
}

/// The text as an ordered run of untouched stretches and slots, so it can
/// be rendered as one continuous sentence with each slot in place — rather
/// than one isolated card per slot repeating most of the sentence each time.
sealed class _Segment {
  const _Segment();
}

class _PlainSegment extends _Segment {
  const _PlainSegment(this.text);
  final String text;
}

class _SlotSegment extends _Segment {
  const _SlotSegment(this.group);
  final _SlotGroup group;
}

/// Mobile counterpart of the front end's vote page: the same two mirrored
/// readings of the text — the current wording struck through, then every
/// competing proposal in its place — over the same api routes.
///
/// The one deliberate departure is how a vote is offered. The front opens
/// its choice on hover, which has no equivalent here, so tapping a proposal
/// opens a sheet with the same two options.
class VotePage extends StatefulWidget {
  const VotePage({super.key, required this.textId});

  final String textId;

  @override
  State<VotePage> createState() => _VotePageState();
}

class _VotePageState extends State<VotePage> {
  String _title = '';
  List<_SlotGroup> _groups = [];
  List<_Segment> _segments = [];
  bool _loading = true;
  String? _error;
  bool _canVote = false;

  /// Kept for the lifetime of the page and disposed at the end: the spans
  /// are rebuilt on every setState, but their recognizers must not be —
  /// each one leaks if replaced without being disposed.
  final Map<String, TapGestureRecognizer> _recognizers = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final recognizer in _recognizers.values) {
      recognizer.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    // Permission first, but never fatal: failing to read it only means the
    // vote controls stay hidden, which is the safe side to err on.
    VoteApi.canVote().then((canVote) {
      if (mounted) setState(() => _canVote = canVote);
    }).catchError((_) {});

    try {
      final text = await VoteApi.textWithSlots(widget.textId);
      _title = text.title;

      if (text.slots.isEmpty) {
        // No open round is a normal state, not an error.
        if (mounted) setState(() => _loading = false);
        return;
      }

      final fragmentsPerSlot = await Future.wait(
        text.slots.map((slot) => VoteApi.fragmentsForSlot(widget.textId, slot.id)),
      );

      // Best-effort, unlike the fragments: a vote that couldn't be read
      // back only costs the highlight on an earlier choice, where failing
      // the whole page would cost the vote itself.
      var myVotes = <String, String>{};
      try {
        myVotes = await VoteApi.myVotes(widget.textId);
      } catch (_) {}

      // Rune offsets, not code units: slot bounds are counted in runes, and
      // slicing a string with accents any other way cuts through characters.
      final runes = text.content.runes.toList();
      final groups = <_SlotGroup>[];
      for (var i = 0; i < text.slots.length; i++) {
        final slot = text.slots[i];
        final fragments = fragmentsPerSlot[i];
        groups.add(
          _SlotGroup(
            slot: slot,
            original: String.fromCharCodes(runes.sublist(slot.start, slot.end)),
            seedFragmentId: fragments.where((f) => f.isSeed).firstOrNull?.id,
            proposals: fragments.where((f) => !f.isSeed).toList(),
          )..votedFragmentId = myVotes[slot.id],
        );
      }

      if (!mounted) return;
      setState(() {
        _groups = groups;
        _segments = _buildSegments(runes, groups);
        _loading = false;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => (_error = e.message, _loading = false));
    } catch (_) {
      if (mounted) setState(() => (_error = 'Chargement impossible.', _loading = false));
    }
  }

  /// Walks the slots left to right — queel guarantees they never overlap
  /// within a round — interleaving the plain text that falls between them.
  List<_Segment> _buildSegments(List<int> runes, List<_SlotGroup> groups) {
    final sorted = [...groups]..sort((a, b) => a.slot.start.compareTo(b.slot.start));
    final segments = <_Segment>[];
    var cursor = 0;
    for (final group in sorted) {
      if (group.slot.start > cursor) {
        segments.add(_PlainSegment(String.fromCharCodes(runes.sublist(cursor, group.slot.start))));
      }
      segments.add(_SlotSegment(group));
      cursor = group.slot.end;
    }
    if (cursor < runes.length) {
      segments.add(_PlainSegment(String.fromCharCodes(runes.sublist(cursor))));
    }
    return segments;
  }

  Future<void> _vote(_SlotGroup group, String? fragmentId) async {
    if (fragmentId == null || group.voting) return;

    setState(() {
      group.voting = true;
      group.error = null;
    });

    try {
      await VoteApi.castVote(fragmentId);
      if (mounted) setState(() => group.votedFragmentId = fragmentId);
    } on ApiException catch (e) {
      if (mounted) setState(() => group.error = e.message);
    } catch (_) {
      if (mounted) setState(() => group.error = 'Erreur lors du vote.');
    } finally {
      if (mounted) setState(() => group.voting = false);
    }
  }

  /// The tap equivalent of the front end's hover popup: the same two
  /// choices, for this proposal's slot.
  void _openVoteSheet(_SlotGroup group, Fragment fragment) {
    showModalBottomSheet<void>(
      context: context,
      builder: (sheetContext) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
              child: Text('« ${group.original} » → « ${fragment.content} »'),
            ),
            ListTile(
              leading: const Icon(Icons.undo),
              title: const Text('Voter pour le texte original'),
              enabled: group.seedFragmentId != null,
              onTap: () {
                Navigator.of(sheetContext).pop();
                _vote(group, group.seedFragmentId);
              },
            ),
            ListTile(
              leading: const Icon(Icons.edit),
              title: const Text('Voter pour la modification'),
              onTap: () {
                Navigator.of(sheetContext).pop();
                _vote(group, fragment.id);
              },
            ),
          ],
        ),
      ),
    );
  }

  TapGestureRecognizer _recognizerFor(_SlotGroup group, Fragment fragment) {
    return _recognizers.putIfAbsent(
      fragment.id,
      () => TapGestureRecognizer()..onTap = () => _openVoteSheet(group, fragment),
    );
  }

  /// The text as it stands, every slot's current wording struck through.
  /// Never interactive — voting for the original is offered in the sheet,
  /// alongside the proposal it competes with.
  TextSpan _originalSpan() {
    return TextSpan(
      children: [
        for (final segment in _segments)
          switch (segment) {
            _PlainSegment(:final text) => TextSpan(text: text),
            _SlotSegment(:final group) => TextSpan(
                text: group.original,
                style: _votedFor(group, group.seedFragmentId)
                    // Voting for the original keeps it: the strike-through
                    // would say the opposite of the highlight.
                    ? _chosenStyle
                    : TextStyle(
                        backgroundColor: AppColors.removedTint,
                        color: AppColors.removedInk,
                        decoration: TextDecoration.lineThrough,
                      ),
              ),
          },
      ],
    );
  }

  /// How a wording this user voted for is painted — the same yellow the
  /// web front uses (#fef08a), so "chosen" reads the same on both.
  static final _chosenStyle = TextStyle(
    backgroundColor: Colors.yellow.shade200,
    color: Colors.yellow.shade900,
    fontWeight: FontWeight.w600,
  );

  /// Whether this user's vote in that slot went to that fragment.
  ///
  /// Takes a nullable id so the originals line can ask about
  /// seedFragmentId directly: a slot queel somehow didn't seed has a null
  /// one, and null must never match a null vote.
  bool _votedFor(_SlotGroup group, String? fragmentId) =>
      fragmentId != null && group.votedFragmentId == fragmentId;

  /// The same reconstruction, mirrored: each slot shows every competing
  /// proposal side by side in its place, separated by " / ".
  TextSpan _proposalsSpan() {
    final children = <InlineSpan>[];
    for (final segment in _segments) {
      switch (segment) {
        case _PlainSegment(:final text):
          children.add(TextSpan(text: text));
        case _SlotSegment(:final group) when group.proposals.isEmpty:
          children.add(TextSpan(text: group.original));
        case _SlotSegment(:final group):
          for (var i = 0; i < group.proposals.length; i++) {
            final fragment = group.proposals[i];
            children.add(
              TextSpan(
                text: fragment.content,
                style: _votedFor(group, fragment.id)
                    ? _chosenStyle
                    : TextStyle(
                        backgroundColor: AppColors.settledTint,
                        color: AppColors.settledInk,
                      ),
                recognizer: _canVote ? _recognizerFor(group, fragment) : null,
              ),
            );
            if (i < group.proposals.length - 1) children.add(const TextSpan(text: ' / '));
          }
      }
    }
    return TextSpan(children: children);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: QueelAppBar(title: _title.isEmpty ? 'Voter' : 'Voter — $_title'),
      body: _body(),
    );
  }

  Widget _body() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error case final error?) {
      return Center(child: Text(error, style: TextStyle(color: AppColors.removedInk)));
    }
    if (_groups.isEmpty) {
      return const Center(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text(
            "Aucun round n'est actuellement ouvert pour ce texte.",
            textAlign: TextAlign.center,
          ),
        ),
      );
    }

    final theme = Theme.of(context).textTheme;
    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Valeurs initiales', style: theme.labelLarge),
          const SizedBox(height: 8),
          Text.rich(_originalSpan()),
          const SizedBox(height: 24),
          Text('Valeurs modifiées', style: theme.labelLarge),
          const SizedBox(height: 8),
          Text.rich(_proposalsSpan()),
          if (_canVote) ...[
            const SizedBox(height: 8),
            Text(
              'Touchez une proposition pour voter.',
              style: theme.bodySmall?.copyWith(color: Colors.grey),
            ),
          ],
          for (final group in _groups) ...[
            if (group.votedFragmentId case final votedId?)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  '« ${group.original} » : vote enregistré pour '
                  '${votedId == group.seedFragmentId ? 'le texte original' : 'la modification'}.',
                  style: const TextStyle(color: AppColors.settledInk),
                ),
              ),
            if (group.error case final error?)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  '« ${group.original} » : $error',
                  style: TextStyle(color: AppColors.removedInk),
                ),
              ),
          ],
        ],
      ),
    );
  }
}
