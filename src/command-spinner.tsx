import { Spinner } from "./spinner";
import type { Theme } from "./theme";

// ── command spinner ──────────────────────────────────────────────────────────
//
// The async-command wiring (story 10-13): it adapts a raw operation and a
// command's copy into the Spinner primitive, so every slow command gets its
// pending/success/failure line for free. The operation resolves to nothing; this
// turns that into the command's success line, and turns a rejection into the
// command's summarized failure line. update-check has a dynamic outcome (latest
// vs. a pending update), so it wires the Spinner directly rather than through
// this common path.

/** A command's copy: fixed pending/success text and a failure formatter. */
export interface CommandCopy {
  pending: string;
  success: string;
  failure: (error: unknown) => string;
}

export interface CommandSpinnerProps {
  copy: CommandCopy;
  /** The slow work itself; its resolved value is irrelevant to the line drawn. */
  operation: () => Promise<unknown>;
  nerdfont: boolean;
  theme: Theme;
  onDone?: (ok: boolean) => void;
}

export function CommandSpinner({ copy, operation, nerdfont, theme, onDone }: CommandSpinnerProps) {
  const run = async () => {
    try {
      await operation();
      return copy.success;
    } catch (error) {
      // The Spinner renders a rejection's message as the failure line, so carry
      // the command's summarized failure copy through the Error.
      throw new Error(copy.failure(error));
    }
  };

  return (
    <Spinner pending={copy.pending} run={run} nerdfont={nerdfont} theme={theme} onDone={onDone} />
  );
}
