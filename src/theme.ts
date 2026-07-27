import { z } from "zod";

// ── theme ────────────────────────────────────────────────────────────────────
//
// The single source of colour. Every renderer asks for a semantic *role* and
// gets back an ANSI index 0-15; taskthing never emits an RGB value, so the
// user's own terminal palette (Dracula, Solarized, …) decides the real hue
// (ADR-0005). There is one hardcoded default mapping and a custom override, and
// nothing else — no named presets.

/**
 * The semantic roles every renderer draws through. The catalog is index-only by
 * contract: a role names a *meaning* (an error, the inbox accent, an overdue
 * date), and the theme turns it into one of the 16 ANSI slots.
 */
export const ROLES = [
  "highlight",
  "error",
  "success",
  "warning",
  "text-secondary",
  // The fill behind a highlighted value in a command's outcome line (mehorias
  // item 3): paired with `text-secondary` as the foreground, it makes the values
  // a user just entered read as a subtle chip.
  "background-secondary",
  "inbox-accent",
  // Date buckets (story 19/20): overdue and yesterday are danger shades, today
  // and tomorrow are standout highlights; later/future dates reuse
  // `text-secondary`.
  "date-overdue",
  "date-yesterday",
  "date-today",
  "date-tomorrow",
] as const;

export type Role = (typeof ROLES)[number];

/**
 * The default role→index map. It follows the common ANSI convention
 * (1≈red/error, 2≈green/success, 3≈yellow/warning, 4≈blue/highlight), so it
 * reads well on most terminals with no configuration (story 2).
 */
const DEFAULT_MAP: Record<Role, number> = {
  highlight: 4,
  error: 1,
  success: 2,
  warning: 3,
  "text-secondary": 8,
  // Black by default, so the secondary chip sits quietly behind the gray value;
  // remap it in a custom theme for a more prominent fill.
  "background-secondary": 0,
  "inbox-accent": 6,
  // Overdue is a dark danger, yesterday a lighter (bright) danger; today is a
  // standout yellow, tomorrow a standout blue — all inside the 0-15 contract.
  "date-overdue": 1,
  "date-yesterday": 9,
  "date-today": 11,
  "date-tomorrow": 12,
};

export interface Theme {
  /** The ANSI index (0-15) a semantic role resolves to. */
  color(role: Role): number;
}

/**
 * A custom override: every key must be a known role and every value an ANSI
 * index 0-15. An unknown role or an out-of-range/non-integer index is a broken
 * mapping the user should hear about, not something to silently drop (story 4).
 */
const customThemeSchema = z.partialRecord(
  z.enum(ROLES),
  z.number().int().min(0).max(15),
);

/**
 * The theme every renderer draws through. With no argument it is the default
 * map; given a custom role→index JSON it overrides only the roles that JSON
 * names, leaving the rest at their default (story 3) — an override, never a
 * wholesale replacement.
 */
export function createTheme(customJson?: string): Theme {
  const map = { ...DEFAULT_MAP };

  if (customJson) {
    const overrides = customThemeSchema.parse(JSON.parse(customJson));
    for (const [role, index] of Object.entries(overrides)) {
      map[role as Role] = index as number;
    }
  }

  return {
    color(role) {
      return map[role];
    },
  };
}
