import { Text } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import type { MessageSegment } from "./task-message";
import type { Theme } from "./theme";

// ── outcome line ──────────────────────────────────────────────────────────────
//
// Draws a mutation command's success line (mehorias items 2-4) — for tasks and
// boards alike, since both reduce to the same shape: the success glyph, then the
// message segments, with the variable spans (a title/name, a date, a recurrence,
// a new icon/color) in the secondary foreground on a secondary background, so the
// values the user just entered read as a subtle chip against the fixed words. The
// plain/variable split is decided upstream in task-message/board-message; this
// only paints it. Colour goes through the theme (role → ANSI index → ink name),
// never RGB (ADR-0005).

export interface OutcomeLineProps {
  segments: MessageSegment[];
  nerdfont: boolean;
  theme: Theme;
}

export function OutcomeLine({ segments, nerdfont, theme }: OutcomeLineProps) {
  const glyphs = createGlyphs(nerdfont);
  const foreground = inkColor(theme.color("text-secondary"));
  const background = inkColor(theme.color("background-secondary"));

  return (
    <Text>
      <Text color={inkColor(theme.color("success"))}>{glyphs.success}</Text>{" "}
      {segments.map((segment, i) =>
        segment.variable ? (
          <Text key={i} color={foreground} backgroundColor={background}>
            {segment.text}
          </Text>
        ) : (
          <Text key={i}>{segment.text}</Text>
        ),
      )}
    </Text>
  );
}
