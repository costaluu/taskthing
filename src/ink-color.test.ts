import { test, expect } from "bun:test";

import { inkColor } from "./ink-color";

// The theme is index-only (ADR-0005): it hands renderers an ANSI index 0-15, and
// this helper is the one place that turns that index into a colour name ink/chalk
// understands — still pure ANSI-16, never an RGB value. The expected names are
// chalk's own standard palette names, an independent source of truth.

test("each ANSI index maps to its conventional ink colour name", () => {
  expect(inkColor(0)).toBe("black");
  expect(inkColor(1)).toBe("red");
  expect(inkColor(2)).toBe("green");
  expect(inkColor(3)).toBe("yellow");
  expect(inkColor(4)).toBe("blue");
  expect(inkColor(5)).toBe("magenta");
  expect(inkColor(6)).toBe("cyan");
  expect(inkColor(7)).toBe("white");
  // 8-15 are the bright half; 8 is chalk's `gray` (bright black).
  expect(inkColor(8)).toBe("gray");
  expect(inkColor(9)).toBe("redBright");
  expect(inkColor(10)).toBe("greenBright");
  expect(inkColor(11)).toBe("yellowBright");
  expect(inkColor(12)).toBe("blueBright");
  expect(inkColor(13)).toBe("magentaBright");
  expect(inkColor(14)).toBe("cyanBright");
  expect(inkColor(15)).toBe("whiteBright");
});

test("an index outside 0-15 is rejected, not silently undefined", () => {
  // The theme already clamps to 0-15, but this is a public boundary — an index
  // outside the ANSI range is a bug to surface, never a stray `undefined` colour
  // that ink would ignore.
  expect(() => inkColor(16)).toThrow();
  expect(() => inkColor(-1)).toThrow();
  expect(() => inkColor(2.5)).toThrow();
});
