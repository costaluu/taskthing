import * as chrono from "chrono-node";

// ── natural-language date parsing ───────────────────────────────────────────
//
// Wraps chrono-node with two corrections it doesn't make on its own:
//
// - Which order a slash date reads in. Chrono's own default is month-first
//   (American): `10/08/2026` is October 8th. The "europe" dateFormat from
//   config.md reads day-first instead, via chrono's built-in `en.GB` locale,
//   so the same string means August 10th — matching what the user configured,
//   not chrono's default.
//
// - The clock time chrono fills in when the input names no time of its own.
//   Left alone, chrono implies the reference instant's time-of-day (whatever
//   moment `add` happened to run), which is a meaningless hour to store on a
//   task the user only ever gave a date. When no time was named, this pins
//   the task to that calendar date at UTC midnight instead.
export function parseNaturalDate(
  input: string,
  now: Date,
  dateFormat: "america" | "europe",
): Date | null {
  const engine = dateFormat === "europe" ? chrono.en.GB : chrono;
  const [result] = engine.parse(input, now);
  if (result === undefined) return null;
  if (result.start.isCertain("hour")) return result.start.date();

  const year = result.start.get("year")!;
  const month = result.start.get("month")!;
  const day = result.start.get("day")!;
  return new Date(Date.UTC(year, month - 1, day));
}
