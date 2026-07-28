import { Box, Text, useInput } from "ink";

import { createGlyphs } from "./glyph";
import type { Theme } from "./theme";

// ── confirmation UI ──────────────────────────────────────────────────────────
//
// A y/n gate for destructive or important actions (story 32-35): a small
// standout title, a selector glyph, the question (already phrased and ending in
// `?` by the caller — e.g. the parsed recurrence for `set date-time`), and a
// `(y/n)` prompt. It reports the answer as a boolean; it decides nothing about
// the action itself, and by design offers no bypass path (init 1.3 has none).

export interface ConfirmationProps {
  title: string;
  /** Already phrased and ending in `?`; drawn verbatim so the user can vet it. */
  question: string;
  nerdfont: boolean;
  theme: Theme;
  onSubmit: (answer: boolean) => void;
}

export function Confirmation({ title, question, nerdfont, onSubmit }: ConfirmationProps) {
  const glyphs = createGlyphs(nerdfont);

  useInput((input) => {
    const key = input.toLowerCase();
    if (key === "y") onSubmit(true);
    else if (key === "n") onSubmit(false);
  });

  return (
    <Box flexDirection="column">
      <Text bold>{title}</Text>
      <Box>
        <Text>{glyphs.selector} </Text>
        <Text>{question} </Text>
        <Text>(y/n) </Text>
      </Box>
    </Box>
  );
}
