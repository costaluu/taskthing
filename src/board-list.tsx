import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import { INBOX } from "./mdwal";
import type { Theme } from "./theme";

// ── board list ───────────────────────────────────────────────────────────────
//
// Draws the plain data Spec 0003's `boards` produces (story 26-28): a "BOARDS"
// title and each board numbered from the kv_store, underlined + bold in its own
// colour with its icon and name. The virtual inbox uses its `inbox-accent` role
// and glyph, consistent with the task view.

export interface BoardRow {
  number: number;
  board: { id: string; name: string; icon: string; color: string };
}

export interface BoardListProps {
  rows: BoardRow[];
  nerdfont: boolean;
  theme: Theme;
}

export function BoardList({ rows, nerdfont, theme }: BoardListProps) {
  const glyphs = createGlyphs(nerdfont);

  return (
    <Box flexDirection="column">
      <Text bold color={inkColor(theme.color("highlight"))}>
        BOARDS
      </Text>
      {rows.length === 0 && (
        <Text color={inkColor(theme.color("text-secondary"))}>
          no boards here
        </Text>
      )}
      {rows.map((row) => {
        const isInbox = row.board.id === INBOX;
        const icon = isInbox ? glyphs.inbox : row.board.icon;
        const color = isInbox
          ? inkColor(theme.color("inbox-accent"))
          : row.board.color;
        return (
          <Text key={row.board.id}>
            {row.number}.{" "}
            <Text bold underline color={color}>
              {icon} {row.board.name}
            </Text>
          </Text>
        );
      })}
    </Box>
  );
}
