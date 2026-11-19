package main

import (
	"os"
	"strings"
	"testing"
)

const fixtureTest = `# group: Smileys & Emotion
1F600                                                  ; fully-qualified     # 😀 E1.0 grinning face
263A                                                   ; unqualified         # ☺ E0.6 smiling face
263A FE0F                                              ; fully-qualified     # ☺️ E0.6 smiling face
# group: People & Body
1F44B 1F3FD                                            ; fully-qualified     # 👋🏽 E1.0 waving hand: medium skin tone
1F468 200D 1F4BB                                       ; fully-qualified     # 👨‍💻 E4.0 man technologist
1F3FB                                                  ; component           # 🏻 E1.0 light skin tone
# group: Objects
1F6D2                                                  ; fully-qualified     # 🛒 E3.0 shopping cart
1F1E7 1F1F7                                            ; fully-qualified     # 🇧🇷 E2.0 flag: Brazil
1F9EA                                                  ; fully-qualified     # 🧪 E11.0 test tube
`

const fixtureAnnotations = `{"annotations":{"identity":{"language":"en"},"annotations":{
 "😀":{"default":["face","grin"],"tts":["grinning face"]},
 "☺":{"default":["face","relaxed","smile"],"tts":["smiling face"]},
 "🛒":{"default":["cart","shopping","trolley"],"tts":["shopping cart"]},
 "👋":{"default":["hand","wave"],"tts":["waving hand"]},
 "{":{"default":["brace"],"tts":["open curly bracket"]}
}}}`

func TestBuild(t *testing.T) {
	out, err := build(strings.NewReader(fixtureAnnotations), strings.NewReader(fixtureTest))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"😀\tgrinning face\tface|grin",
		"☺️\tsmiling face\tface|relaxed|smile", // key in the CLDR file has no FE0F
		"🛒\tshopping cart\tcart|shopping|trolley",
		"🇧🇷\tflag: Brazil\t", // not annotated: name comes from emoji-test
		"🧪\ttest tube\t",
	}, "\n") + "\n"
	if string(out) != want {
		t.Fatalf("table mismatch\n got: %q\nwant: %q", out, want)
	}
}

func TestBuildDropsUnqualifiedSkinToneZWJAndComponents(t *testing.T) {
	out, _ := build(strings.NewReader(fixtureAnnotations), strings.NewReader(fixtureTest))
	for _, bad := range []string{"👋🏽", "👨‍💻", "🏻", "{"} {
		if strings.Contains(string(out), bad) {
			t.Errorf("%q must not be in the table", bad)
		}
	}
	if strings.Count(string(out), "☺") != 1 {
		t.Errorf("the unqualified form of ☺ must not add a second row:\n%s", out)
	}
}

func TestBuildRejectsEmptyResult(t *testing.T) {
	if _, err := build(strings.NewReader(fixtureAnnotations), strings.NewReader("# nothing\n")); err == nil {
		t.Fatal("an emoji-test file with no usable rows must be an error, not an empty table")
	}
}

// The committed table must be exactly what the committed sources generate.
func TestTableIsUpToDate(t *testing.T) {
	ann, err := os.Open("source/annotations-en.json")
	if err != nil {
		t.Fatal(err)
	}
	defer ann.Close()
	list, err := os.Open("source/emoji-test.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer list.Close()
	want, err := build(ann, list)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../table.tsv")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("internal/emoji/table.tsv is stale: run `go generate ./internal/emoji`")
	}
}
