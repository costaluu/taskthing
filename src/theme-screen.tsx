import { useState } from "react";
import { Box, Text, useInput } from "ink";

import { createGlyphs } from "./glyph";
import { inkColor } from "./ink-color";
import { TaskList, type TaskGroup } from "./task-list";
import { INBOX } from "./mdwal";
import { createTheme, ROLES, type Role, type Theme } from "./theme";

// ── theme screen ─────────────────────────────────────────────────────────────
//
// `theme` (story 45-46) and the config menu's theme entry (mehorias item 9). The
// old default-vs-custom chooser is replaced by a live editor: one row per
// semantic role, each showing a colour swatch and the ANSI index it maps to.
// ←/→ recolour the focused role (still bound to the 16 ANSI slots — never RGB,
// ADR-0005), ↑/↓ move between roles, and a dummy task list under the rows
// previews the theme in real time. It returns the config `theme` value — "" when
// the map is back at the default, else the role→index JSON of the overrides.

export interface ThemeScreenProps {
  theme: Theme;
  onSubmit: (themeValue: string) => void;
}

/** The default index for every role, to diff a custom map against on submit. */
const DEFAULTS = createTheme();

/** Every element a default workspace can show, so each role is visible at once. */
function previewGroups(now: Date): TaskGroup[] {
  const day = 24 * 60 * 60 * 1000;
  const task = (over: Partial<import("./schema").Task>): import("./schema").Task => ({
    id: over.title!,
    board: INBOX,
    completed: false,
    title: over.title!,
    star: false,
    rrule: null,
    description: null,
    deleted: false,
    ...over,
  });

  return [
    {
      board: { id: INBOX, name: "inbox", icon: "", color: "" },
      rows: [
        {
          number: 1,
          task: task({ title: "team standup", star: true, rrule: "RRULE:FREQ=WEEKLY" }),
          date: now,
        },
        { number: 2, task: task({ title: "call the dentist" }), date: new Date(now.getTime() + day) },
        {
          number: 3,
          task: task({ title: "ship the release", completed: true, description: "notes" }),
          date: new Date(now.getTime() - day),
        },
      ],
    },
    {
      board: { id: "work", name: "work", icon: "★", color: "magenta" },
      rows: [
        { number: 4, task: task({ title: "pay the invoice", board: "work" }), date: new Date(now.getTime() - 3 * day) },
      ],
    },
  ];
}

export function ThemeScreen({ theme, onSubmit }: ThemeScreenProps) {
  // Seed the editable map from the theme handed in, so the editor opens on the
  // config's current colours — default or an existing custom mapping.
  const [map, setMap] = useState<Record<Role, number>>(
    () => Object.fromEntries(ROLES.map((role) => [role, theme.color(role)])) as Record<Role, number>,
  );
  const [cursor, setCursor] = useState(0);

  useInput((_input, key) => {
    const role = ROLES[cursor]!;
    if (key.downArrow) setCursor((c) => Math.min(c + 1, ROLES.length - 1));
    else if (key.upArrow) setCursor((c) => Math.max(c - 1, 0));
    // ←/→ cycle the focused role through the 16 ANSI slots, wrapping at the ends.
    else if (key.leftArrow) setMap((m) => ({ ...m, [role]: (m[role] + 15) % 16 }));
    else if (key.rightArrow) setMap((m) => ({ ...m, [role]: (m[role] + 1) % 16 }));
    else if (key.return) {
      // A role left at its default is not an override: only the diff is saved, so
      // an untouched map submits as "" (the default theme).
      const overrides = Object.fromEntries(
        ROLES.filter((r) => map[r] !== DEFAULTS.color(r)).map((r) => [r, map[r]]),
      );
      onSubmit(Object.keys(overrides).length === 0 ? "" : JSON.stringify(overrides));
    }
  });

  const glyphs = createGlyphs(false);
  const secondary = inkColor(theme.color("text-secondary"));
  // The preview draws through the map being edited, so every keystroke recolours
  // it live; the swatches and rows keep drawing through the passed-in theme.
  const preview: Theme = { color: (role) => map[role] };
  const width = Math.max(...ROLES.map((r) => r.length));
  const now = new Date();

  return (
    <Box flexDirection="column">
      <Text bold>Theme</Text>
      <Text color={secondary}>↑/↓ role   ←/→ colour   enter save</Text>
      <Text> </Text>
      {ROLES.map((role, i) => {
        const current = i === cursor;
        const selector = current ? glyphs.selector : " ".repeat([...glyphs.selector].length);
        return (
          <Text key={role}>
            {selector} <Text backgroundColor={inkColor(map[role])}> </Text> {role.padEnd(width)} {"< ansi "}
            {map[role]}
            {" >"}
          </Text>
        );
      })}
      <Text> </Text>
      <TaskList groups={previewGroups(now)} now={now} dateFormat="america" nerdfont={false} theme={preview} />
    </Box>
  );
}
