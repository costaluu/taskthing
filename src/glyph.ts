// ── glyph ────────────────────────────────────────────────────────────────────
//
// Every visible mark in taskthing has two forms: a nerdfont glyph and a
// plain-unicode fallback. This module is the single place that chooses between
// them, driven by the user's `nerdfont` config flag (defaulting to no-nerdfont,
// so nothing renders as tofu). The nerdfont codepoints and the fallbacks both
// come verbatim from CONTEXT.md — the source of truth for the visual language.
// Nerdfont forms are written as `\u{...}` escapes so the codepoint is exact and
// legible even though the glyph itself lives in the Private Use Area.

interface GlyphPair {
  fallback: string;
  nerdfont: string;
}

const PAIRS = {
  checkboxUnchecked: { fallback: "[ ]", nerdfont: "\u{f096}" },
  checkboxChecked: { fallback: "[x]", nerdfont: "\u{f046}" },
  inbox: { fallback: "📬", nerdfont: "\u{f01c}" },
  star: { fallback: "⭐", nerdfont: "\u{f005}" },
  description: { fallback: "📄", nerdfont: "\u{f0188}" },
  recurrence: { fallback: "🔄", nerdfont: "\u{f0e2}" },
  selector: { fallback: "→", nerdfont: "\u{f178}" },
  dotUnselected: { fallback: "○", nerdfont: "\u{f10c}" },
  dotSelected: { fallback: "●", nerdfont: "\u{f192}" },
  success: { fallback: "✓", nerdfont: "\u{f05d}" },
  failure: { fallback: "𐄂", nerdfont: "\u{f52f}" },
  info: { fallback: "ℹ️", nerdfont: "\u{f05d}" },
  warn: { fallback: "⚠️", nerdfont: "\u{ea6c}" },
} satisfies Record<string, GlyphPair>;

export type Glyphs = Record<keyof typeof PAIRS, string>;

/** Resolve every glyph to its nerdfont form or its fallback, per the flag. */
export function createGlyphs(nerdfont: boolean): Glyphs {
  const resolved = {} as Glyphs;
  for (const [name, pair] of Object.entries(PAIRS)) {
    resolved[name as keyof typeof PAIRS] = nerdfont
      ? pair.nerdfont
      : pair.fallback;
  }
  return resolved;
}
