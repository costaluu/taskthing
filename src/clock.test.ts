import { test, expect } from "bun:test";

import { createMonotonicClock } from "./clock";

// The clock seam is pure: tests drive it with an injected wall clock and the
// seed a caller read from the last events.log line, then assert only on the
// sequence of timestamps it yields.
function makeWallClock(values: bigint[]): () => bigint {
  let i = 0;
  return () => {
    const v = values[i++];
    if (v === undefined) throw new Error("wall clock exhausted");
    return v;
  };
}

test("a backward wall clock still yields strictly increasing timestamps", () => {
  // NTP correction, a manual clock change, or a stalled clock: real time goes
  // back behind what this peer already logged.
  const clock = createMonotonicClock({
    now: makeWallClock([9000n, 4000n, 4000n, 9000n, 20000n]),
    seed: 5000n,
  });

  // Ahead of the seed → the wall clock. Then it steps back behind both the seed
  // and what we just issued, and stalls: each timestamp is the previous one + 1,
  // never a repeat and never a step back.
  expect(clock()).toBe(9000n);
  expect(clock()).toBe(9001n);
  expect(clock()).toBe(9002n);
  // Even a wall clock equal to an already-issued timestamp doesn't repeat it.
  expect(clock()).toBe(9003n);
  // Once real time overtakes the correction, the wall clock takes over again.
  expect(clock()).toBe(20000n);
});

test("the first timestamp never repeats the seed read from the log", () => {
  const clock = createMonotonicClock({ now: makeWallClock([5000n]), seed: 5000n });

  // The seed is the last ts already in events.log; reusing it would break the
  // strictly-increasing log that purge relies on.
  expect(clock()).toBe(5001n);
});

test("the clock yields the wall clock while it runs ahead of the last local timestamp", () => {
  const clock = createMonotonicClock({
    now: makeWallClock([5000n, 6000n]),
    seed: 1000n,
  });

  // Nothing to correct: real time is past everything this peer has logged, so
  // the timestamp is the wall clock itself, not a synthetic increment.
  expect(clock()).toBe(5000n);
  expect(clock()).toBe(6000n);
});
