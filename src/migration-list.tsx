import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import type { Theme } from "./theme";

// CONTEXT's copy carries typos ("peding", "verify our"); the corrected spelling
// from Spec 0004 story 31 is used here.
const NO_PENDING = "this workspace has no migrations pending";
const PENDING =
  "there's pending migrations. verify your taskthing installation.";

// ── migration list ───────────────────────────────────────────────────────────
//
// Draws the plain data Spec 0003's `migrations` produces (story 29-31): a
// "MIGRATIONS" title, each known migration numbered from the kv_store as
// `<version> <applied?>`, and a closing validation message that tells the user
// whether every migration has been applied.

export interface MigrationRow {
  number: number;
  version: string;
  applied: boolean;
}

export interface MigrationListProps {
  rows: MigrationRow[];
  nerdfont: boolean;
  theme: Theme;
}

export function MigrationList({ rows, nerdfont, theme }: MigrationListProps) {
  const glyphs = createGlyphs(nerdfont);
  const pending = rows.some((row) => !row.applied);

  return (
    <Box flexDirection="column">
      <Text bold color={inkColor(theme.color("highlight"))}>
        MIGRATIONS
      </Text>
      {rows.map((row) => (
        <Text key={row.version}>
          {row.number}. {row.version} {row.applied ? "yes" : "no"}
        </Text>
      ))}
      {pending ? (
        <Text color={inkColor(theme.color("warning"))}>
          {glyphs.warn} {PENDING}
        </Text>
      ) : (
        <Text color={inkColor(theme.color("text-secondary"))}>
          {glyphs.info} {NO_PENDING}
        </Text>
      )}
    </Box>
  );
}
