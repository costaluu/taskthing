// Package emoji searches the Unicode emoji list by name and CLDR keyword.
// The data is generated, not fetched: see gen/ and NOTICE.
package emoji

import (
	_ "embed"
	"sort"
	"strings"
	"unicode"
)

//go:generate go run ./gen

//go:embed table.tsv
var tableTSV string

// Entry is one emoji: the glyph to store, its CLDR name and keywords.
type Entry struct {
	Glyph    string
	Name     string
	Keywords []string
}

type indexed struct {
	Entry
	nameWords []string
	keywords  []string // lower-cased
}

var (
	all     []indexed
	byGlyph = map[string]int{}
)

func init() {
	for _, line := range strings.Split(tableTSV, "\n") {
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		e := indexed{Entry: Entry{Glyph: cols[0], Name: cols[1]}}
		if len(cols) > 2 && cols[2] != "" {
			e.Keywords = strings.Split(cols[2], "|")
		}
		e.nameWords = words(e.Name)
		for _, k := range e.Keywords {
			e.keywords = append(e.keywords, strings.ToLower(k))
		}
		byGlyph[e.Glyph] = len(all)
		all = append(all, e)
	}
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// A word of the query scores by the best way it matches an emoji; every word
// must match something, and the scores add up.
const (
	nameExact     = 4
	keywordExact  = 3
	namePrefix    = 2
	keywordPrefix = 1
)

func (e *indexed) score(word string) int {
	best := 0
	for _, w := range e.nameWords {
		switch {
		case w == word:
			return nameExact
		case strings.HasPrefix(w, word):
			best = max(best, namePrefix)
		}
	}
	for _, k := range e.keywords {
		switch {
		case k == word:
			best = max(best, keywordExact)
		case strings.HasPrefix(k, word):
			best = max(best, keywordPrefix)
		}
	}
	return best
}

// Search returns up to limit emoji matching every word of query, best first;
// equal scores keep the Unicode list's own order. An empty query matches
// nothing.
func Search(query string, limit int) []Entry {
	q := words(query)
	if len(q) == 0 {
		return nil
	}
	type hit struct{ at, score int }
	var hits []hit
next:
	for i := range all {
		total := 0
		for _, w := range q {
			s := all[i].score(w)
			if s == 0 {
				continue next
			}
			total += s
		}
		hits = append(hits, hit{i, total})
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = all[h.at].Entry
	}
	return out
}

// common is what the picker shows before anything is typed: icons people
// tend to give a board.
var common = []string{
	"🏠", "💼", "📚", "🛒", "💰", "✈️", "🏋️", "🎮", "🎵", "🍳",
	"❤️", "⭐", "🔥", "💡", "🎯", "📅", "✅", "🧹", "🚗", "🐶",
	"🌱", "🧳", "🎓", "💻", "📝", "🎨", "🏥", "🎁",
}

// Common returns the starter list. A glyph missing from the table comes back
// with an empty Name, which the tests catch.
func Common() []Entry {
	out := make([]Entry, len(common))
	for i, g := range common {
		if at, ok := byGlyph[g]; ok {
			out[i] = all[at].Entry
		} else {
			out[i] = Entry{Glyph: g}
		}
	}
	return out
}
