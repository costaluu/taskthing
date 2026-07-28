import { RRule } from "rrule";
import { z } from "zod";

import { INBOX } from "./mdwal";

// ── domain schemas ──────────────────────────────────────────────────────────
//
// The zod schemas are the single source of truth for field types: the YAML
// frontmatter carries bare values and no type information, so a field's type
// can be changed in exactly one place. mdwal derives each field's LWW companions
// (`<field>_lastModified` / `_lastModifiedBy`) and the read-only `createdAt` /
// `updatedAt` from these — none of which are schema fields themselves.

/**
 * A task's whole temporal dimension: `DTSTART` is its date, `RRULE` its
 * recurrence. A dated but non-recurring task is a `DTSTART` with no rule, and an
 * undated one is null.
 *
 * The *stored* form is the RFC 5545 string, so a field round-trips through the
 * frontmatter unchanged — a schema that parsed it into an `RRule` object would
 * write that object back out. Consumers that need the object build it with
 * `rruleSchema`.
 */
export const rruleStringSchema = z.string().refine(
  (str) => {
    try {
      RRule.fromString(str);
      return true;
    } catch {
      return false;
    }
  },
  { message: "not a valid recurrence rule" },
);

/** The stored string as a usable rule. */
export const rruleSchema = rruleStringSchema.transform((str) => RRule.fromString(str));

export const taskSchema = z.object({
  id: z.string().nonempty(),
  /**
   * A real board's nanoid, or the `inbox` sentinel for the virtual board every
   * task falls into when it has none (ADR-0004).
   */
  board: z.string().nonempty().default(INBOX),
  completed: z.boolean().default(false),
  title: z.string().nonempty(),
  star: z.boolean().default(false),
  rrule: rruleStringSchema.nullable().default(null),
  description: z.string().nullable().default(null),
  deleted: z.boolean().default(false),
});

export type Task = z.infer<typeof taskSchema>;

export const boardSchema = z.object({
  id: z.string().nonempty(),
  name: z.string().nonempty(),
  /** How the board shows up in a listing; an ANSI 16 colour name (ADR-0005). */
  icon: z.string().default(""),
  color: z.string().default(""),
  deleted: z.boolean().default(false),
});

export type Board = z.infer<typeof boardSchema>;

/** A migration as applied to a workspace: the version it shipped at, and its log. */
export const migrationSchema = z.object({
  version: z.string().nonempty(),
  content: z.string().nonempty(),
});

export type Migration = z.infer<typeof migrationSchema>;

/**
 * `config.md`: the settings that live outside any workspace and outside LWW.
 * `currentWorkspace` is stored here too, but is not a setting — it moves only
 * through `use`, never as a side effect of configuring something else.
 */
export const configSchema = z.object({
  currentWorkspace: z.string().nonempty(),
  /** How dates are drawn; the renderer has to know how to lay each one out. */
  dateFormat: z.enum(["america", "europe"]).default("america"),
  /** Whether this terminal can draw nerdfont glyphs; assumed no until told. */
  nerdfont: z.boolean().default(false),
  theme: z.string().default(""),
  // ── binary settings (Spec 0005) ──
  /** Epoch ms of the last update check; null until the first one. */
  lastUpdateCheck: z.number().nullable().default(null),
  /** The cached answer: is a newer release waiting, and which version. */
  updateAvailable: z.boolean().default(false),
  updateTarget: z.string().nullable().default(null),
  /**
   * Whether `update apply` asks first (default) or updates unattended. Defaults
   * to confirm because an update may trigger an irreversible migration (story 19).
   */
  autoUpdate: z.enum(["confirm", "silent"]).default("confirm"),
  // ── automatic maintenance cadence (mehorias item 10) ──
  //
  // Three nested levels, each counted in the unit below it: a sync runs on a
  // wall-clock interval, a rebuild every so many syncs, a truncate every so many
  // rebuilds. The defaults are taskthing's baseline — sync every 4h, rebuild once
  // per 5 syncs, truncate once per 10 rebuilds.
  /** Hours between automatic syncs. */
  autoSync: z.number().int().positive().default(4),
  /** Syncs between automatic rebuilds. */
  autoRebuild: z.number().int().positive().default(5),
  /** Rebuilds between automatic truncates. */
  autoTruncate: z.number().int().positive().default(10),
});

export type Config = z.infer<typeof configSchema>;

/**
 * The keys `config set` may write: the display settings plus the auto-update
 * mode. The update cache (lastUpdateCheck/updateAvailable/updateTarget) is
 * maintained by the updater, not set by hand; the current workspace moves only
 * through `use`.
 */
export const CONFIG_KEYS = [
  "dateFormat",
  "nerdfont",
  "theme",
  "autoUpdate",
  "autoSync",
  "autoRebuild",
  "autoTruncate",
] as const;
