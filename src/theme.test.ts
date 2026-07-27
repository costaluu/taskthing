import { test, expect } from "bun:test";

import { ROLES, createTheme } from "./theme";

// The theme module is the single source of colour in taskthing: every renderer
// asks it for a semantic role and gets back an ANSI index 0-15 — never an RGB
// value (ADR-0005), so the user's own terminal palette resolves the real hue.
// Tests drive the public resolver and assert on the index a role maps to.

test("the default theme maps every role to an ANSI index, by convention", () => {
  const theme = createTheme();

  // The default follows the common ANSI convention, so it looks right on most
  // terminals with no configuration (story 2).
  expect(theme.color("error")).toBe(1);
  expect(theme.color("success")).toBe(2);
  expect(theme.color("warning")).toBe(3);
  expect(theme.color("highlight")).toBe(4);

  // Whatever the catalog grows to, no role may escape the 0-15 range — that is
  // the whole ADR-0005 contract that keeps taskthing off RGB.
  for (const role of ROLES) {
    const index = theme.color(role);
    expect(Number.isInteger(index)).toBe(true);
    expect(index).toBeGreaterThanOrEqual(0);
    expect(index).toBeLessThanOrEqual(15);
  }
});

test("a custom theme overrides only the roles it names, the rest fall back", () => {
  // The user tweaks a couple of roles without restating the whole map (story 3).
  const theme = createTheme('{ "highlight": 5, "error": 9 }');

  expect(theme.color("highlight")).toBe(5);
  expect(theme.color("error")).toBe(9);

  // Everything the JSON left out keeps the default — it is an override, not a
  // replacement.
  expect(theme.color("success")).toBe(2);
  expect(theme.color("text-secondary")).toBe(8);
});

test("a custom theme is rejected when a role or index is invalid", () => {
  // An unknown role is a typo the user should hear about, not a silently
  // ignored key (story 4).
  expect(() => createTheme('{ "hihglight": 4 }')).toThrow(/hihglight/);

  // Indices are ANSI slots 0-15 — the ADR-0005 contract. Anything outside that,
  // or a non-integer, is a broken mapping, not a colour.
  expect(() => createTheme('{ "highlight": 16 }')).toThrow();
  expect(() => createTheme('{ "highlight": -1 }')).toThrow();
  expect(() => createTheme('{ "highlight": 4.5 }')).toThrow();

  // Malformed JSON fails loudly rather than degrading into an empty override.
  expect(() => createTheme("{ not json")).toThrow();
});
