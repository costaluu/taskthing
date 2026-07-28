// ── ink colour adapter ───────────────────────────────────────────────────────
//
// The theme is index-only (ADR-0005): it resolves a semantic role to an ANSI
// index 0-15 and nothing else, so colour assertions stay terminal-independent.
// This helper is the single bridge from that index to a colour name ink/chalk
// draws with — still one of the 16 ANSI slots, never an RGB value. Renderers
// compose it as `inkColor(theme.color(role))`.

// The 16 ANSI slots as chalk's standard foreground names. 0-7 are the base
// colours, 8-15 the bright half (8 being chalk's `gray`, i.e. bright black).
// prettier-ignore
const NAMES = [
  "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "gray", "redBright", "greenBright", "yellowBright",
  "blueBright", "magentaBright", "cyanBright", "whiteBright",
] as const;

/** The ink/chalk colour name for an ANSI index 0-15. */
export function inkColor(index: number): string {
  const name = NAMES[index];
  if (name === undefined || !Number.isInteger(index)) {
    throw new Error(`ANSI colour index out of range (0-15): ${index}`);
  }
  return name;
}
