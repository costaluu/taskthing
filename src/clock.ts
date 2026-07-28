// ── per-peer monotonic clock (ADR-0006) ─────────────────────────────────────
//
// Every mutating operation consumes one timestamp from here. The clock is pure:
// the wall clock is injected, and so is the seed — the last timestamp this peer
// already wrote, which a caller reads from the final events.log line. Keeping
// the log strictly increasing is what makes timestamp-based purge safe.

export interface MonotonicClockConfig {
  /** Wall clock, in nanoseconds since the UTC epoch. */
  now: () => bigint;
  /** The greatest timestamp this peer has already logged (0 for a fresh log). */
  seed: bigint;
}

export function createMonotonicClock(config: MonotonicClockConfig): () => bigint {
  let last = config.seed;
  return () => {
    // max(now, last + 1): real time when it leads, otherwise the smallest step
    // that keeps this peer's sequence strictly increasing (ADR-0006).
    const now = config.now();
    const next = now > last ? now : last + 1n;
    last = next;
    return next;
  };
}
