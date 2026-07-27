import { useState } from "react";
import { Box, Text, useInput } from "ink";

import type { Theme } from "./theme";

// ── text input / textarea ────────────────────────────────────────────────────
//
// The surface for free-form edits: `set` values single-line, and the
// custom-theme JSON as a multiline textarea (story 40-41). Single-line submits
// on Enter; multiline treats Enter as a newline and submits on Ctrl+D (a lone
// Escape is ambiguous with escape sequences, so it is not the submit key). A
// `validate` hook (used by the theme textarea) shows a live error as you type.

export interface TextInputProps {
  placeholder?: string;
  /** When true, Enter inserts a newline and Escape submits (a textarea). */
  multiline?: boolean;
  /** Live validation: return an error message to show, or null when valid. */
  validate?: (text: string) => string | null;
  nerdfont: boolean;
  theme: Theme;
  onSubmit: (text: string) => void;
}

export function TextInput({ placeholder, multiline, validate, onSubmit }: TextInputProps) {
  const [value, setValue] = useState("");
  const error = validate ? validate(value) : null;

  useInput((input, key) => {
    if (multiline && key.ctrl && input === "d") {
      onSubmit(value);
    } else if (key.return) {
      if (multiline) setValue((v) => v + "\n");
      else onSubmit(value);
    } else if (key.backspace || key.delete) {
      setValue((v) => v.slice(0, -1));
    } else if (input && !key.ctrl && !key.escape) {
      setValue((v) => v + input);
    }
  });

  return (
    <Box flexDirection="column">
      {value === "" && placeholder !== undefined ? (
        <Text dimColor>{placeholder}</Text>
      ) : (
        <Text>{value}</Text>
      )}
      {error !== null && <Text>{error}</Text>}
    </Box>
  );
}
