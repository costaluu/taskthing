import { Box, Text } from "ink";

import { createGlyphs } from "./glyph";
import { formatTaskDate } from "./date-format";
import { inkColor } from "./ink-color";
import { recurs } from "./recurrence";
import { INBOX } from "./mdwal";
import type { Theme } from "./theme";
import type { Task } from "./schema";

// ── task list ────────────────────────────────────────────────────────────────
//
// Draws the plain data Spec 0003's `list` produces (story 14-25): a "Tasks"
// title, tasks grouped under their board header, each line numbered from the
// kv_store and laid out as `<check> <date> <star> <title> <recurring>`. It only
// draws — no command logic, no kv_store, no git. Colour goes through the theme
// (role → ANSI index → ink colour name), never RGB (ADR-0005).

export interface TaskRow {
  /** The kv_store number the user acts on. */
  number: number;
  task: Task;
  /** Resolved date: DTSTART for an open task, completion instant for a done one, null if undated. */
  date: Date | null;
}

export interface TaskGroup {
  board: { id: string; name: string; icon: string; color: string };
  rows: TaskRow[];
  /** When set (a global listing), the workspace the board lives in, drawn as "(name)". */
  workspace?: string;
}

export interface TaskListProps {
  groups: TaskGroup[];
  now: Date;
  dateFormat: "america" | "europe";
  nerdfont: boolean;
  theme: Theme;
}

export function TaskList({
  groups,
  now,
  dateFormat,
  nerdfont,
  theme,
}: TaskListProps) {
  const glyphs = createGlyphs(nerdfont);

  return (
    <Box flexDirection="column">
      <Text bold backgroundColor={inkColor(theme.color("highlight"))}>
        {" "}
        TASKS{" "}
      </Text>
      {groups.length === 0 && (
        <Text color={inkColor(theme.color("text-secondary"))}>
          no tasks here
        </Text>
      )}
      {groups.map((group) => {
        // The virtual inbox draws its own glyph and accent role, never a passed
        // icon (story 16); a real board draws its own icon in its own colour.
        const isInbox = group.board.id === INBOX;
        const icon = isInbox ? glyphs.inbox : group.board.icon;
        const headerColor = isInbox
          ? inkColor(theme.color("inbox-accent"))
          : group.board.color;
        return (
          <>
            <Text> </Text>
            <Box key={`${group.workspace ?? ""}/${group.board.id}`} flexDirection="column">
              <Text bold underline color={headerColor}>
                {icon} {group.board.name}
                {group.workspace !== undefined && ` (${group.workspace})`}
              </Text>
              {group.rows.map((row) => {
                const check = row.task.completed ? (
                  <Text color={inkColor(theme.color("success"))}>
                    {glyphs.checkboxChecked}
                  </Text>
                ) : (
                  glyphs.checkboxUnchecked
                );
                const date = formatTaskDate({
                  date: row.date,
                  now,
                  completed: row.task.completed,
                  dateFormat,
                });
                const starred = row.task.star;
                const described = row.task.description !== null;
                const recurring =
                  row.task.rrule !== null && recurs(row.task.rrule);
                return (
                  <Text key={row.task.id}>
                    {row.number}. {check}{" "}
                    {date !== null && (
                      <Text underline color={inkColor(theme.color(date.role))}>
                        {date.text}
                      </Text>
                    )}{" "}
                    {starred && (
                      <Text color={inkColor(theme.color("warning"))}>
                        {glyphs.star}{" "}
                      </Text>
                    )}
                    {row.task.title}
                    {described && (
                      <Text color={inkColor(theme.color("text-secondary"))}>
                        {" "}
                        {glyphs.description}
                      </Text>
                    )}
                    {recurring && (
                      <Text color={inkColor(theme.color("highlight"))}>
                        {" "}
                        {glyphs.recurrence}
                      </Text>
                    )}
                  </Text>
                );
              })}
            </Box>
          </>
        );
      })}
    </Box>
  );
}
