/// authorId queel stamps on the fragment it seeds every new slot with — the
/// untouched original wording, standing as the implicit "keep as-is"
/// competitor. It is never listed as a proposal to diff against itself;
/// it's what the "voter pour le texte original" button votes for.
const seedAuthorId = 'seed';

/// One editable range of a text, carved out for the current round.
/// [start] and [end] are rune offsets into the text's content, never byte
/// or UTF-16 offsets — slicing with anything else mangles accents.
class Slot {
  Slot({required this.id, required this.start, required this.end, required this.round});

  factory Slot.fromJson(Map<String, dynamic> json) {
    return Slot(
      id: json['id'] as String,
      start: json['start'] as int,
      end: json['end'] as int,
      round: json['round'] as int,
    );
  }

  final String id;
  final int start;
  final int end;
  final int round;
}

/// One competing wording proposed for a slot.
class Fragment {
  Fragment({required this.id, required this.content, required this.authorId});

  factory Fragment.fromJson(Map<String, dynamic> json) {
    return Fragment(
      id: json['id'] as String,
      content: json['content'] as String,
      authorId: json['authorId'] as String,
    );
  }

  final String id;
  final String content;
  final String authorId;

  bool get isSeed => authorId == seedAuthorId;
}

/// api's GET /api/texts/{id}/with-slots response.
class TextWithSlots {
  TextWithSlots({required this.title, required this.content, required this.roundNumber, required this.slots});

  factory TextWithSlots.fromJson(Map<String, dynamic> json) {
    final text = json['text'] as Map<String, dynamic>;
    return TextWithSlots(
      title: text['title'] as String,
      content: text['content'] as String,
      roundNumber: json['roundNumber'] as int,
      slots: (json['slots'] as List? ?? []).map((s) => Slot.fromJson(s as Map<String, dynamic>)).toList(),
    );
  }

  final String title;
  final String content;
  final int roundNumber;
  final List<Slot> slots;
}
