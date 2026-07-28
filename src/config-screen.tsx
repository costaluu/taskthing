import { useState } from "react";
import { Box, Text, useInput } from "ink";

import { Confirmation } from "./confirmation";
import { Selection } from "./selection";
import { ThemeScreen } from "./theme-screen";
import { inkColor } from "./ink-color";
import type { Config } from "./schema";
import type { Theme } from "./theme";

// ── config screen ────────────────────────────────────────────────────────────
//
// `taskthing config` with no args (story 44): a menu of every configurable key
// except the current workspace — date format, nerdfont, theme — each routing to
// its editor. The workspace is deliberately absent because it moves only through
// `use` (Spec 0003). It draws the screens and returns the partial config update;
// writing config.md is Spec 0003.
//
// Every level carries a help line and can be left at any time (mehorias items
// 8-9): esc steps back to the menu (or exits, from the menu), q exits outright.
// Leaving without a choice returns an empty update, which the caller writes as a
// no-op.

type EditableKey = "dateFormat" | "nerdfont" | "theme";

export type ConfigUpdate = Partial<Pick<Config, EditableKey>>;

export interface ConfigScreenProps {
  theme: Theme;
  onSubmit: (update: ConfigUpdate) => void;
}

/** The bottom help line for each level: the editor's own keys, then back/exit. */
const HELP: Record<"menu" | EditableKey, string> = {
  menu: "↑/↓ move · enter select · q quit",
  dateFormat: "↑/↓ move · enter save · esc back · q quit",
  nerdfont: "y/n select · esc back · q quit",
  theme: "esc back · q quit",
};

export function ConfigScreen({ theme, onSubmit }: ConfigScreenProps) {
  const [key, setKey] = useState<EditableKey | null>(null);

  // Back and exit stay available at every level. The child editors never bind esc
  // or q, so this handler and theirs can both be live without fighting over keys:
  // esc returns to the menu (or leaves, from the menu itself), q leaves outright.
  useInput((input, k) => {
    if (k.escape) {
      if (key === null) onSubmit({});
      else setKey(null);
    } else if (input === "q") {
      onSubmit({});
    }
  });

  const screen =
    key === null ? (
      <Selection
        title="Config"
        items={[
          { label: "date format", value: "dateFormat" },
          { label: "nerdfont", value: "nerdfont" },
          { label: "theme", value: "theme" },
        ]}
        nerdfont={false}
        theme={theme}
        onSubmit={([value]) => setKey(value as EditableKey)}
      />
    ) : key === "dateFormat" ? (
      <Selection
        title="Date format"
        items={[
          { label: "american (yyyy/mm/dd)", value: "america" },
          { label: "europe (dd/mm/yyyy)", value: "europe" },
        ]}
        nerdfont={false}
        theme={theme}
        onSubmit={([value]) => onSubmit({ dateFormat: value as "america" | "europe" })}
      />
    ) : key === "nerdfont" ? (
      <Confirmation
        title="Nerdfont"
        question="enable nerdfont support?"
        nerdfont={false}
        theme={theme}
        onSubmit={(nerdfont) => onSubmit({ nerdfont })}
      />
    ) : (
      <ThemeScreen theme={theme} onSubmit={(value) => onSubmit({ theme: value })} />
    );

  return (
    <Box flexDirection="column">
      {screen}
      <Text> </Text>
      <Text color={inkColor(theme.color("text-secondary"))}>{HELP[key ?? "menu"]}</Text>
    </Box>
  );
}
