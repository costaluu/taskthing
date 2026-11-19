// Command gen builds internal/emoji/table.tsv from the Unicode emoji list and
// the CLDR English annotations kept in ./source. It runs under `go generate`
// and never touches the network.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type annotation struct {
	Default []string `json:"default"`
	TTS     []string `json:"tts"`
}

// build joins emoji-test.txt (which says which emoji exist, in what order, and
// their fallback name) with the CLDR annotations (names and keywords). Only
// fully-qualified single-codepoint-run emoji are kept: no skin tones, no ZWJ
// sequences, no components.
func build(annotations, emojiTest io.Reader) ([]byte, error) {
	var doc struct {
		Annotations struct {
			Annotations map[string]annotation `json:"annotations"`
		} `json:"annotations"`
	}
	if err := json.NewDecoder(annotations).Decode(&doc); err != nil {
		return nil, fmt.Errorf("annotations: %w", err)
	}
	// CLDR keys omit the variation selector (U+FE0F); emoji-test includes it.
	notes := map[string]annotation{}
	for k, v := range doc.Annotations.Annotations {
		notes[strings.ReplaceAll(k, "️", "")] = v
	}

	var out bytes.Buffer
	rows := 0
	sc := bufio.NewScanner(emojiTest)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		codes, rest, ok := strings.Cut(line, ";")
		if !ok {
			continue
		}
		status, comment, _ := strings.Cut(rest, "#")
		if strings.TrimSpace(status) != "fully-qualified" {
			continue
		}
		var glyph strings.Builder
		skip := false
		for _, hex := range strings.Fields(codes) {
			cp, err := strconv.ParseUint(hex, 16, 32)
			if err != nil {
				return nil, fmt.Errorf("bad code point %q", hex)
			}
			if cp == 0x200D || (cp >= 0x1F3FB && cp <= 0x1F3FF) {
				skip = true
				break
			}
			glyph.WriteRune(rune(cp))
		}
		if skip {
			continue
		}
		// "<emoji> E1.0 grinning face": the name is everything after the version.
		fields := strings.Fields(comment)
		if len(fields) < 3 {
			return nil, fmt.Errorf("no name in %q", line)
		}
		name := strings.Join(fields[2:], " ")
		keywords := ""
		if a, ok := notes[strings.ReplaceAll(glyph.String(), "️", "")]; ok {
			if len(a.TTS) > 0 {
				name = a.TTS[0]
			}
			keywords = strings.Join(a.Default, "|")
		}
		fmt.Fprintf(&out, "%s\t%s\t%s\n", glyph.String(), name, keywords)
		rows++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, errors.New("emoji-test.txt produced no usable rows")
	}
	return out.Bytes(), nil
}

func main() {
	ann, err := os.Open("gen/source/annotations-en.json")
	if err != nil {
		fatal(err)
	}
	defer ann.Close()
	list, err := os.Open("gen/source/emoji-test.txt")
	if err != nil {
		fatal(err)
	}
	defer list.Close()
	table, err := build(ann, list)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile("table.tsv", table, 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "emoji gen:", err)
	os.Exit(1)
}
