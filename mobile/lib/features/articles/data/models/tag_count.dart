/// One entry of GET /api/tags: a label and how many current articles carry
/// it — the count being what tells a reader whether crossing it with
/// another will leave anything.
class TagCount {
  TagCount({required this.tag, required this.count});

  factory TagCount.fromJson(Map<String, dynamic> json) {
    return TagCount(tag: json['tag'] as String, count: json['count'] as int);
  }

  final String tag;
  final int count;
}
