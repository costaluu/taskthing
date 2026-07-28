import { test, expect } from "bun:test";
import { RRule } from "rrule";

import { recurrenceToText } from "./recurrence";

// recurrenceToText renders a stored rrule as the human phrasing the `add`
// feedback shows after the next occurrence (mehorias item 2, <recurrence-to-string>).
// The expected phrasings are the spec of how a rule should read, not values
// recomputed from the library.

test("recurrenceToText renders a recurring rule as human-readable text", () => {
  const weekly = new RRule({ freq: RRule.WEEKLY, byweekday: RRule.MO }).toString();
  expect(recurrenceToText(weekly)).toBe("every week on Monday");

  const daily = new RRule({ freq: RRule.DAILY }).toString();
  expect(recurrenceToText(daily)).toBe("every day");

  const monthly = new RRule({ freq: RRule.MONTHLY }).toString();
  expect(recurrenceToText(monthly)).toBe("every month");
});
