# Sources of table.tsv

Both files are copied verbatim, not fetched at build time. To update, replace
them, run `go generate ./internal/emoji` and commit all three files.

| File | Origin | Version | sha256 |
| --- | --- | --- | --- |
| `annotations-en.json` | https://github.com/unicode-org/cldr-json, `cldr-json/cldr-annotations-full/annotations/en/annotations.json` | CLDR 49.0.0 | `6b6b6e0e5f211bc31cf3fa39f385d80dea6cd19ee19bbc6db9071fce3ae5d360` |
| `emoji-test.txt` | https://unicode.org/Public/emoji/latest/emoji-test.txt | Unicode Emoji 18.0 (2026-04-30) | `8f3735cda1f92a779d78af67cf86066bb1f07143dc22f2ac29394d9bc57ab21a` |

Kept in the table: `fully-qualified` rows without ZWJ (U+200D) or skin-tone
modifiers (U+1F3FB..1F3FF). Names and keywords come from CLDR; flags are not in
that file, so their name comes from `emoji-test.txt`.
