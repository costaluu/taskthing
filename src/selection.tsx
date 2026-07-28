import { useState } from "react";
import { Box, Text, useInput } from "ink";

import { createGlyphs } from "./glyph";
import type { Theme } from "./theme";

// ── selection UI ─────────────────────────────────────────────────────────────
//
// Picks from finite choices (story 36-39): each item shows a selected/unselected
// dot, the current item carries a selector glyph and is recoloured. It supports
// mono-select (one) and multi-select, and always returns an *array* of the
// chosen values — a mono-select is a one-element array, so every caller has the
// same return shape. Multi-select is currently speculative (no shipping command
// uses it) but kept per CONTEXT.

export interface SelectionItem {
  label: string;
  value: string;
}

export interface SelectionProps {
  title: string;
  items: SelectionItem[];
  /** Mono by default (one choice); multi lets the user toggle several. */
  multi?: boolean;
  nerdfont: boolean;
  theme: Theme;
  onSubmit: (selected: string[]) => void;
}

export function Selection({ title, items, multi, nerdfont, onSubmit }: SelectionProps) {
  const glyphs = createGlyphs(nerdfont);
  const [cursor, setCursor] = useState(0);
  // Multi-select's toggle set, independent of the cursor. Unused in mono.
  const [chosen, setChosen] = useState<Set<number>>(new Set());

  useInput((input, key) => {
    if (key.downArrow) setCursor((c) => Math.min(c + 1, items.length - 1));
    else if (key.upArrow) setCursor((c) => Math.max(c - 1, 0));
    else if (multi && input === " ") {
      setChosen((prev) => {
        const next = new Set(prev);
        if (next.has(cursor)) next.delete(cursor);
        else next.add(cursor);
        return next;
      });
    } else if (key.return) {
      if (multi) {
        // The toggled items, in item order.
        onSubmit(items.filter((_, i) => chosen.has(i)).map((it) => it.value));
      } else {
        // Mono-select: the cursor is the choice, returned as a one-element array.
        onSubmit([items[cursor]!.value]);
      }
    }
  });

  return (
    <Box flexDirection="column">
      <Text bold>{title}</Text>
      {items.map((item, i) => {
        const current = i === cursor;
        // Mono: the cursor is the selection, so the current item is filled.
        // Multi: the dot reflects the toggle set, independent of the cursor.
        const isSelected = multi ? chosen.has(i) : current;
        const dot = isSelected ? glyphs.dotSelected : glyphs.dotUnselected;
        const selector = current ? glyphs.selector : " ".repeat([...glyphs.selector].length);
        return (
          <Text key={item.value}>
            {selector} {dot} {item.label}
          </Text>
        );
      })}
    </Box>
  );
}
