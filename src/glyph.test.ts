import { test, expect } from "bun:test";

import { createGlyphs } from "./glyph";

// Every visible glyph in taskthing has two forms: a nerdfont form and a
// plain-unicode fallback. The glyph module is the one place that decides which
// to use, driven by the `nerdfont` config flag. Tests drive the resolver and
// assert on the exact character it hands back — the fallbacks and nerdfont
// codepoints both come verbatim from CONTEXT.md, never recomputed here.

test("without nerdfont, every glyph is its plain-unicode fallback", () => {
  // The default: assume no nerdfont until the user confirms, so nothing renders
  // as tofu (story 9).
  const g = createGlyphs(false);

  expect(g.checkboxUnchecked).toBe("[ ]");
  expect(g.checkboxChecked).toBe("[x]");
  expect(g.inbox).toBe("📬");
  expect(g.star).toBe("⭐");
  expect(g.description).toBe("📄");
  expect(g.recurrence).toBe("🔄");
  expect(g.selector).toBe("→");
  expect(g.dotUnselected).toBe("○");
  expect(g.dotSelected).toBe("●");
  expect(g.success).toBe("✓");
  expect(g.failure).toBe("𐄂");
  expect(g.info).toBe("ℹ️");
  expect(g.warn).toBe("⚠️");
});

test("with nerdfont, every glyph is its Private-Use-Area nerdfont form", () => {
  // When the user has nerdfonts, the richer glyphs replace the fallbacks — same
  // codepoints CONTEXT.md pins.
  const g = createGlyphs(true);

  expect(g.checkboxUnchecked).toBe("\u{f096}");
  expect(g.checkboxChecked).toBe("\u{f046}");
  expect(g.inbox).toBe("\u{f01c}");
  expect(g.star).toBe("\u{f005}");
  expect(g.description).toBe("\u{f0188}");
  expect(g.recurrence).toBe("\u{f0e2}");
  expect(g.selector).toBe("\u{f178}");
  expect(g.dotUnselected).toBe("\u{f10c}");
  expect(g.dotSelected).toBe("\u{f192}");
  expect(g.success).toBe("\u{f05d}");
  expect(g.failure).toBe("\u{f52f}");
  expect(g.info).toBe("\u{f05d}");
  expect(g.warn).toBe("\u{ea6c}");
});
