import { RRule } from "rrule";

import { parseNaturalDate } from "./date-parse";

// ── the add string-input parser ─────────────────────────────────────────────
//
// One line in, a task out:
// `add "walk the dog d:[tomorrow] r:[every monday] b:[chores]"`. The brackets are
// part of the pattern, not decoration — `d:`/`date:`/`r:`/`recurrence:`/`b:`/
// `board:` are only recognised when followed by a closing `[…]`. That is what
// lets free text like "remind Ed: buy milk" stay a title untouched, and what
// makes stripping a matched tag trivial: tag and brackets go together.

const TAGS = {
  date: /\b(?:d|date):\[([^\]]*)\]/i,
  recurrence: /\b(?:r|recurrence):\[([^\]]*)\]/i,
  board: /\b(?:b|board):\[([^\]]*)\]/i,
};

export interface ParsedAddInput {
  title: string;
  /** The task's whole temporal dimension, or null when it has no date. */
  rrule: string | null;
  /** The board the user named (by name/number/"inbox"), or null to default to the inbox. */
  board: string | null;
}

export function parseAddInput(
  input: string,
  now: Date,
  dateFormat: "america" | "europe",
): ParsedAddInput {
  let rest = input;
  const take = (pattern: RegExp): string | null => {
    const match = rest.match(pattern);
    if (match === null) return null;
    rest = rest.replace(pattern, "");
    return match[1]!.trim();
  };

  const date = take(TAGS.date);
  const recurrence = take(TAGS.recurrence);
  // A board tag with empty brackets (`b:[]`) names no board, so it is dropped
  // like an absent tag and the task falls to the inbox default.
  const boardTag = take(TAGS.board);
  const board = boardTag === null || boardTag === "" ? null : boardTag;

  const dtstart = date === null ? null : parseNaturalDate(date, now, dateFormat);
  if (date !== null && dtstart === null) {
    throw new Error(`not a date: ${date}`);
  }

  const title = rest.trim().replace(/\s{2,}/g, " ");
  if (recurrence === null) {
    return { title, rrule: dtstart === null ? null : new RRule({ dtstart }).toString(), board };
  }

  const rule = RRule.fromText(recurrence);
  if (rule.origOptions.freq === undefined) {
    throw new Error(`not a recurrence: ${recurrence}`);
  }

  // A recurrence with no date given starts from now: the rule has to run from
  // somewhere, and "every monday" said today means starting today.
  return {
    title,
    rrule: new RRule({ ...rule.origOptions, dtstart: dtstart ?? now }).toString(),
    board,
  };
}
