import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import type { Theme } from "./theme";

// ── workspace list ────────────────────────────────────────────────────────────
//
// Draws `workspace list` (mehorias item 6) in the Selection UI's shape — the
// same selector/dot/label rows the multi-select uses — but static: there is
// nothing to pick, only to show. The current workspace is the "selected" row
// (filled dot, selector glyph, recoloured), the way every unscoped command needs
// the user to know which one it acts on. Colour goes through the theme
// (role → ANSI index → ink name), never RGB (ADR-0005).

export interface WorkspaceListProps {
  workspaces: string[];
  current: string;
  nerdfont: boolean;
  theme: Theme;
}

export function WorkspaceList({ workspaces, current, nerdfont, theme }: WorkspaceListProps) {
  const glyphs = createGlyphs(nerdfont);
  // The selector column stays aligned on non-current rows by blanking its width.
  const blank = " ".repeat([...glyphs.selector].length);

  return (
    <Box flexDirection="column">
      <Text bold color={inkColor(theme.color("highlight"))}>
        Workspaces
      </Text>
      {workspaces.length === 0 && (
        <Text color={inkColor(theme.color("text-secondary"))}>no workspaces here</Text>
      )}
      {workspaces.map((name) => {
        const isCurrent = name === current;
        const dot = isCurrent ? glyphs.dotSelected : glyphs.dotUnselected;
        const selector = isCurrent ? glyphs.selector : blank;
        return (
          <Text key={name} color={isCurrent ? inkColor(theme.color("highlight")) : undefined}>
            {selector} {dot} {name}
          </Text>
        );
      })}
    </Box>
  );
}
