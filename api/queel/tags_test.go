package queel

import (
	"strings"
	"testing"
)

// TestParseTags pins the one line every client has to read the same way.
// Two of them splitting it differently would file the same text under two
// different sets of labels, and nothing downstream could tell.
func TestParseTags(t *testing.T) {
	cases := []struct {
		name string
		line string
		want []string
	}{
		// Both shapes of the same line: people type the hash in front of a
		// tag, and it is also the separator, so a leading one yields an
		// empty piece that has to be dropped rather than kept.
		{"leading hashes", "#loi #vote", []string{"loi", "vote"}},
		{"separator only", "loi#vote", []string{"loi", "vote"}},
		{"spaces around", "  #  loi  #  vote  ", []string{"loi", "vote"}},

		{"empty line", "", nil},
		{"hashes only", "###", nil},

		// Labels exist to bring texts together; "Loi" filed apart from
		// "loi" would defeat that at the first capital letter.
		{"case folded and deduped", "#Loi #loi #LOI", []string{"loi"}},

		{"order is the author's", "#zeta #alpha", []string{"zeta", "alpha"}},
		{"accents kept", "#écologie", []string{"écologie"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTags(tc.line)
			if len(got) != len(tc.want) {
				t.Fatalf("ParseTags(%q) = %v, want %v", tc.line, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ParseTags(%q) = %v, want %v", tc.line, got, tc.want)
				}
			}
		})
	}

	// Never nil, so a Text always marshals its tags as [] rather than null.
	if ParseTags("") == nil {
		t.Fatal("ParseTags must return an empty slice, not nil")
	}
}

// TestParseTagsBounds covers the caps. A mis-typed line is trimmed rather
// than refused: it is not worth failing a text creation over.
func TestParseTagsBounds(t *testing.T) {
	var line strings.Builder
	for i := 0; i < MaxTags+5; i++ {
		line.WriteString("#tag")
		line.WriteString(string(rune('a' + i)))
	}
	if got := ParseTags(line.String()); len(got) != MaxTags {
		t.Fatalf("kept %d tags, want the cap of %d", len(got), MaxTags)
	}

	long := "#" + strings.Repeat("é", MaxTagRunes+10)
	got := ParseTags(long)
	if len(got) != 1 {
		t.Fatalf("ParseTags(long) = %v, want one tag", got)
	}
	// Runes, not bytes: a multi-byte label must be cut where a reader
	// would count, and must stay valid text.
	if runes := []rune(got[0]); len(runes) != MaxTagRunes {
		t.Fatalf("tag is %d runes, want %d", len(runes), MaxTagRunes)
	}
}

// TestTextsByTagFollowsTheFork pins that a label is a property of the text
// and not of one version of it: closing a round moves the index to the fork,
// so the filter answers with the version the rest of the app shows.
func TestTextsByTagFollowsTheFork(t *testing.T) {
	engine, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	repo := NewRepository(engine)

	content := "Nous le peuple francais declare."
	text, err := repo.CreateText("Constitution", content, "creator", ParseTags("#loi #vote"))
	if err != nil {
		t.Fatal(err)
	}

	found, err := repo.TextsByTag("loi")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != text.ID {
		t.Fatalf("TextsByTag = %v, want the text just created", found)
	}

	start := strings.Index(content, "francais")
	fragment, err := repo.ProposeEdit(text.ID, start, start+len("francais"), "français", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CastVote(fragment.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	outcome, err := repo.CloseRound(text.ID)
	if err != nil {
		t.Fatal(err)
	}

	found, err = repo.TextsByTag("loi")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("TextsByTag = %v, want exactly the current version", found)
	}
	if found[0].ID != outcome.Text.ID {
		t.Fatalf("TextsByTag returned %s, want the fork %s", found[0].ID, outcome.Text.ID)
	}
	if len(found[0].Tags) != 2 {
		t.Fatalf("the fork carries %v, want the labels of the text it came from", found[0].Tags)
	}

	// The count follows too, rather than reporting the two versions.
	tags, err := repo.Tags()
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if tag.Count != 1 {
			t.Fatalf("tag %q counted %d texts, want 1", tag.Tag, tag.Count)
		}
	}

	// Deleting the text takes its labels out of the index with it.
	if err := repo.DeleteText(outcome.Text.ID); err != nil {
		t.Fatal(err)
	}
	if found, err = repo.TextsByTag("loi"); err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("TextsByTag = %v after deleting the text, want none", found)
	}
	if tags, err = repo.Tags(); err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 {
		t.Fatalf("Tags = %v after deleting the only text, want none", tags)
	}
}
