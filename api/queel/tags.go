package queel

import "strings"

const (
	// MaxTags and MaxTagRunes bound what one line of tags can hold. A
	// label is a handful of words at most; these exist so a client cannot
	// turn the field into arbitrary storage, not because anyone is
	// expected to reach them.
	MaxTags     = 10
	MaxTagRunes = 30
)

// ParseTags reads the single line of "#"-separated labels an author types
// at creation into the list a Text carries.
//
// Written here rather than at the edge because every caller has to agree
// on it: two clients splitting the same line differently would file the
// same text under two different labels, and nothing downstream could tell.
//
// The separator is also the character people naturally type in front of a
// tag, so "#loi #vote" and "loi#vote" both give ["loi", "vote"] — leading
// hashes produce empty pieces, which are dropped like any other.
//
// Lower-cased and de-duplicated: labels exist to bring texts together, and
// "Loi" filed apart from "loi" would defeat that at the first capital
// letter. Order is the author's, since nothing else would be meaningful.
//
// Anything past MaxTags is dropped, and a label longer than MaxTagRunes is
// cut rather than refused: a mis-typed line is not worth failing a text
// creation over.
func ParseTags(line string) []string {
	tags := make([]string, 0, MaxTags)
	seen := make(map[string]bool, MaxTags)

	for _, raw := range strings.Split(line, "#") {
		tag := strings.ToLower(strings.TrimSpace(raw))
		if tag == "" || seen[tag] {
			continue
		}

		if runes := []rune(tag); len(runes) > MaxTagRunes {
			tag = string(runes[:MaxTagRunes])
			if seen[tag] {
				continue
			}
		}

		seen[tag] = true
		if tags = append(tags, tag); len(tags) == MaxTags {
			break
		}
	}
	return tags
}
