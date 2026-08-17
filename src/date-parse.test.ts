import { test, expect, describe } from "bun:test";

import { parseNaturalDate } from "./date-parse";

// parseNaturalDate wraps chrono-node and switches between month-first (america)
// and day-first (europe) via chrono's en.GB locale. These tests verify that the
// dateFormat config actually governs how ambiguous slash dates resolve, and that
// unambiguous inputs (natural language, day>12) stay consistent.

const NOW = new Date("2026-07-15T14:30:00Z");

// ── ambiguous slash dates (both parts ≤ 12) ─────────────────────────────────
// The same string must produce different Date objects depending on dateFormat.

describe("ambiguous slash dates", () => {
  test("03/04/2026 — america: Mar 4, europe: Apr 3", () => {
    const am = parseNaturalDate("03/04/2026", NOW, "america")!;
    const eu = parseNaturalDate("03/04/2026", NOW, "europe")!;
    expect(am.toISOString()).toBe("2026-03-04T00:00:00.000Z");
    expect(eu.toISOString()).toBe("2026-04-03T00:00:00.000Z");
  });

  test("01/12/2026 — america: Jan 12, europe: Dec 1", () => {
    const am = parseNaturalDate("01/12/2026", NOW, "america")!;
    const eu = parseNaturalDate("01/12/2026", NOW, "europe")!;
    expect(am.toISOString()).toBe("2026-01-12T00:00:00.000Z");
    expect(eu.toISOString()).toBe("2026-12-01T00:00:00.000Z");
  });

  test("05/06/2026 — america: May 6, europe: Jun 5", () => {
    const am = parseNaturalDate("05/06/2026", NOW, "america")!;
    const eu = parseNaturalDate("05/06/2026", NOW, "europe")!;
    expect(am.toISOString()).toBe("2026-05-06T00:00:00.000Z");
    expect(eu.toISOString()).toBe("2026-06-05T00:00:00.000Z");
  });

  test("11/12/2026 — america: Nov 12, europe: Dec 11", () => {
    const am = parseNaturalDate("11/12/2026", NOW, "america")!;
    const eu = parseNaturalDate("11/12/2026", NOW, "europe")!;
    expect(am.toISOString()).toBe("2026-11-12T00:00:00.000Z");
    expect(eu.toISOString()).toBe("2026-12-11T00:00:00.000Z");
  });
});

// ── unambiguous slash dates (day > 12) ──────────────────────────────────────
// When one part exceeds 12, only one reading is valid. Both formats should
// converge on the same date — or at least not silently produce the wrong one.

describe("unambiguous slash dates (day > 12)", () => {
  test("25/03/2026 — day > 12, only day-first is valid", () => {
    const eu = parseNaturalDate("25/03/2026", NOW, "europe")!;
    expect(eu.toISOString()).toBe("2026-03-25T00:00:00.000Z");

    // america: month=25 is invalid; chrono may fallback or return null
    const am = parseNaturalDate("25/03/2026", NOW, "america");
    if (am !== null) {
      // If chrono falls back to day-first, it should still be Mar 25.
      // But this is a silent format switch — worth knowing about.
      expect(am.toISOString()).toBe("2026-03-25T00:00:00.000Z");
    }
  });

  test("03/25/2026 — america: Mar 25, europe: day=3 month=25 invalid", () => {
    const am = parseNaturalDate("03/25/2026", NOW, "america")!;
    expect(am.toISOString()).toBe("2026-03-25T00:00:00.000Z");

    // europe tries day=3, month=25 — invalid; chrono may fallback or null
    const eu = parseNaturalDate("03/25/2026", NOW, "europe");
    if (eu !== null) {
      expect(eu.toISOString()).toBe("2026-03-25T00:00:00.000Z");
    }
  });

  test("31/01/2026 — only day-first makes sense", () => {
    const eu = parseNaturalDate("31/01/2026", NOW, "europe")!;
    expect(eu.toISOString()).toBe("2026-01-31T00:00:00.000Z");
  });
});

// ── natural language dates — format-independent ─────────────────────────────
// Words like "tomorrow" or "next monday" should resolve identically regardless
// of dateFormat.

describe("natural language is format-independent", () => {
  test("'tomorrow' resolves the same under both formats", () => {
    const am = parseNaturalDate("tomorrow", NOW, "america")!;
    const eu = parseNaturalDate("tomorrow", NOW, "europe")!;
    expect(am.toISOString()).toBe(eu.toISOString());
    expect(am.toISOString()).toBe("2026-07-16T00:00:00.000Z");
  });

  test("'next friday' resolves the same under both formats", () => {
    const am = parseNaturalDate("next friday", NOW, "america")!;
    const eu = parseNaturalDate("next friday", NOW, "europe")!;
    expect(am.toISOString()).toBe(eu.toISOString());
  });

  test("'august 20' resolves the same under both formats", () => {
    const am = parseNaturalDate("august 20", NOW, "america")!;
    const eu = parseNaturalDate("august 20", NOW, "europe")!;
    expect(am.toISOString()).toBe("2026-08-20T00:00:00.000Z");
    expect(eu.toISOString()).toBe("2026-08-20T00:00:00.000Z");
  });
});

// ── temporal ambiguity: past vs future ───────────────────────────────────────
// When a date has no year, chrono must decide whether the user means the past
// occurrence or the next one. For a task app, past resolution is almost always
// wrong — the user is scheduling, not journaling. These tests pin chrono's
// actual behavior so regressions (or intentional fixes) are visible.
//
// AUG_NOW = 2026-08-17 (Monday)

const AUG_NOW = new Date("2026-08-17T14:30:00Z");

describe("month+day in the past — forwards to next occurrence", () => {
  test("'august 14' on Aug 17 → Aug 14 next year", () => {
    const d = parseNaturalDate("august 14", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-08-14T00:00:00.000Z");
  });

  test("'august 14th' — ordinal form, same forward result", () => {
    const d = parseNaturalDate("august 14th", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-08-14T00:00:00.000Z");
  });

  test("'july 20' on Aug 17 → Jul 20 next year", () => {
    const d = parseNaturalDate("july 20", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-07-20T00:00:00.000Z");
  });

  test("'march 1' on Aug 17 → Mar 1 next year", () => {
    const d = parseNaturalDate("march 1", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-03-01T00:00:00.000Z");
  });

  test("'january 15' on Aug 17 → Jan 15 next year", () => {
    const d = parseNaturalDate("january 15", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-01-15T00:00:00.000Z");
  });
});

describe("month+day in the future — straightforward", () => {
  test("'december 25' on Aug 17 — this year, no ambiguity", () => {
    const d = parseNaturalDate("december 25", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-12-25T00:00:00.000Z");
  });

  test("'september 1' on Aug 17 — this year", () => {
    const d = parseNaturalDate("september 1", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-09-01T00:00:00.000Z");
  });

  test("'august 18' (tomorrow) on Aug 17 — this year", () => {
    const d = parseNaturalDate("august 18", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-18T00:00:00.000Z");
  });
});

describe("today's own date — forwardDate advances past-midnight references", () => {
  // NOW is 14:30 UTC, so "august 17" at midnight is already past → forwards.
  test("'august 17' at 14:30 UTC on Aug 17 → next year", () => {
    const d = parseNaturalDate("august 17", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-08-17T00:00:00.000Z");
  });

  // With NOW at midnight, same-day resolves to today.
  test("'august 17' at 00:00 UTC on Aug 17 → today", () => {
    const midnight = new Date("2026-08-17T00:00:00Z");
    const d = parseNaturalDate("august 17", midnight, "america")!;
    expect(d.toISOString()).toBe("2026-08-17T00:00:00.000Z");
  });
});

describe("bare weekday — forwards to next occurrence", () => {
  // AUG_NOW is Monday. Bare "friday" now gives NEXT Friday.
  test("'friday' on Monday → next Friday", () => {
    const d = parseNaturalDate("friday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-21T00:00:00.000Z");
  });

  test("'sunday' on Monday → next Sunday", () => {
    const d = parseNaturalDate("sunday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-23T00:00:00.000Z");
  });

  test("'monday' on Monday (14:30 UTC) → next Monday", () => {
    const d = parseNaturalDate("monday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-24T00:00:00.000Z");
  });

  test("'tuesday' on Monday → tomorrow", () => {
    const d = parseNaturalDate("tuesday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-18T00:00:00.000Z");
  });
});

describe("explicit past markers — respected, not forwarded", () => {
  test("'last friday' stays in the past", () => {
    const d = parseNaturalDate("last friday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-14T00:00:00.000Z");
  });

  test("'last monday' stays in the past", () => {
    const d = parseNaturalDate("last monday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-10T00:00:00.000Z");
  });

  test("'yesterday' stays in the past", () => {
    const d = parseNaturalDate("yesterday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-16T00:00:00.000Z");
  });

  test("'3 days ago' stays in the past", () => {
    const d = parseNaturalDate("3 days ago", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-14T00:00:00.000Z");
  });

  test("'next friday' still works (not blocked by forward mode)", () => {
    const d = parseNaturalDate("next friday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-28T00:00:00.000Z");
  });

  test("'next monday' on Monday → next week's Monday", () => {
    const d = parseNaturalDate("next monday", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-08-24T00:00:00.000Z");
  });
});

describe("bare month — forwards past months to next year", () => {
  test("'march' on Aug 17 → Mar 1 next year", () => {
    const d = parseNaturalDate("march", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2027-03-01T00:00:00.000Z");
  });

  test("'september' on Aug 17 — already future, stays this year", () => {
    const d = parseNaturalDate("september", AUG_NOW, "america")!;
    expect(d.toISOString()).toBe("2026-09-01T00:00:00.000Z");
  });
});

describe("edge cases", () => {
  test("'february 29' on a non-leap year (2026) → null", () => {
    expect(parseNaturalDate("february 29", AUG_NOW, "america")).toBeNull();
    expect(parseNaturalDate("feb 29", AUG_NOW, "america")).toBeNull();
  });

  test("'the 15th' — chrono does not recognize bare ordinal day", () => {
    expect(parseNaturalDate("the 15th", AUG_NOW, "america")).toBeNull();
  });

  test("'the 20th' — same, returns null", () => {
    expect(parseNaturalDate("the 20th", AUG_NOW, "america")).toBeNull();
  });
});

// ── UTC midnight when no time given ─────────────────────────────────────────

describe("no time → UTC midnight", () => {
  test("slash date without time pins to midnight", () => {
    const d = parseNaturalDate("03/04/2026", NOW, "america")!;
    expect(d.getUTCHours()).toBe(0);
    expect(d.getUTCMinutes()).toBe(0);
  });

  test("'tomorrow' pins to midnight", () => {
    const d = parseNaturalDate("tomorrow", NOW, "america")!;
    expect(d.getUTCHours()).toBe(0);
    expect(d.getUTCMinutes()).toBe(0);
  });
});

// ── explicit time is preserved ──────────────────────────────────────────────

describe("explicit time preserved", () => {
  test("slash date with time keeps the hour", () => {
    const d = parseNaturalDate("03/04/2026 14:30", NOW, "america")!;
    expect(d.getUTCHours()).toBe(14);
    expect(d.getUTCMinutes()).toBe(30);
    // Still Mar 4 under america
    expect(d.getUTCMonth()).toBe(2);
    expect(d.getUTCDate()).toBe(4);
  });

  test("'tomorrow at 9am' keeps the hour", () => {
    const d = parseNaturalDate("tomorrow at 9am", NOW, "america")!;
    expect(d.getUTCHours()).toBe(9);
    expect(d.getUTCDate()).toBe(16);
  });
});
