package queel

import (
	"strings"
	"unicode"
)

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
// Hashes, commas and whitespace all separate, and all of them at once. The
// hash is what people type in front of a label, so "#loi #vote" and
// "loi#vote" both give ["loi", "vote"] — a leading hash produces an empty
// piece, dropped like any other.
//
// Cutting on whitespace too is what makes the hash optional rather than
// load-bearing: "loi vote" would otherwise be one label reading "loi vote",
// silently, because nothing in the line separated anything. That is the
// mistake a first-time author makes, and it produces a label nobody will
// ever match.
//
// The cost is that a label cannot contain a space — which is the same rule
// a hashtag follows anywhere else, and why hyphens exist.
//
// Lower-cased and de-duplicated: labels exist to bring texts together, and
// "Loi" filed apart from "loi" would defeat that at the first capital
// letter. Order is the author's, since nothing else would be meaningful.
//
// Anything past MaxTags is dropped, and a label longer than MaxTagRunes is
// cut rather than refused: a mis-typed line is not worth failing a text
// creation over.
func isTagSeparator(r rune) bool {
	return r == '#' || r == ',' || r == ';' || unicode.IsSpace(r)
}

func ParseTags(line string) []string {
	tags := make([]string, 0, MaxTags)
	seen := make(map[string]bool, MaxTags)

	for _, raw := range strings.FieldsFunc(line, isTagSeparator) {
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
