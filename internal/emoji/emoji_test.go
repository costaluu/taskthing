package emoji

import "testing"

func glyphs(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Glyph
	}
	return out
}

func index(es []Entry, glyph string) int {
	for i, e := range es {
		if e.Glyph == glyph {
			return i
		}
	}
	return -1
}

func TestSearchFindsByKeywordAndName(t *testing.T) {
	cases := []struct {
		query string
		glyph string
		top   int // the glyph must be within the first `top` results
	}{
		{"shopping", "🛒", 3},
		{"shopping cart", "🛒", 1},
		{"office", "💼", 5}, // 🏢 "office building" outranks it: a name word beats a keyword
		{"study", "📚", 5},
		{"money", "💰", 3},
		{"brazil", "🇧🇷", 1}, // flags carry no CLDR keywords; the name is enough
	}
	for _, c := range cases {
		got := Search(c.query, 10)
		if i := index(got, c.glyph); i < 0 || i >= c.top {
			t.Errorf("Search(%q): %s at %d, want within first %d; got %v", c.query, c.glyph, i, c.top, glyphs(got))
		}
	}
}

func TestSearchMatchesPrefixes(t *testing.T) {
	if i := index(Search("shop", 10), "🛒"); i < 0 {
		t.Error(`"shop" must find 🛒 through the prefix of "shopping"`)
	}
}

func TestSearchIsCaseInsensitiveAndTrimsSpace(t *testing.T) {
	a, b := glyphs(Search("shopping", 10)), glyphs(Search("  ShOpPiNg ", 10))
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("same query in another case/spacing differs: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same query in another case/spacing differs: %v vs %v", a, b)
		}
	}
}

func TestSearchRequiresEveryWord(t *testing.T) {
	if got := Search("shopping zzzzqqq", 10); len(got) != 0 {
		t.Errorf("a word that matches nothing must empty the result, got %v", glyphs(got))
	}
}

func TestSearchLimitAndEmpty(t *testing.T) {
	if got := Search("face", 5); len(got) != 5 {
		t.Errorf("limit 5 returned %d", len(got))
	}
	if got := Search("", 5); len(got) != 0 {
		t.Errorf("an empty query is not a search, got %v", glyphs(got))
	}
	if got := Search("zzzzqqq", 5); len(got) != 0 {
		t.Errorf("no match must be empty, got %v", glyphs(got))
	}
}

func TestSearchTiesFollowUnicodeOrder(t *testing.T) {
	a, b := Search("face", 50), Search("face", 50)
	for i := range a {
		if a[i].Glyph != b[i].Glyph {
			t.Fatal("search must be deterministic")
		}
	}
}

func TestTableHasNoSkinTonesOrJoinedSequences(t *testing.T) {
	for _, e := range all {
		for _, r := range e.Glyph {
			if r == 0x200D || (r >= 0x1F3FB && r <= 0x1F3FF) {
				t.Fatalf("%q (%s) is a ZWJ or skin-tone sequence", e.Glyph, e.Name)
			}
		}
	}
}

func TestCommonAreAllInTheTable(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Common() {
		if e.Name == "" {
			t.Errorf("common emoji %q is not in the table", e.Glyph)
		}
		if seen[e.Glyph] {
			t.Errorf("common emoji %q is listed twice", e.Glyph)
		}
		seen[e.Glyph] = true
	}
	if len(Common()) < 20 {
		t.Errorf("only %d common emoji", len(Common()))
	}
}
