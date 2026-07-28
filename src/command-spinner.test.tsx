import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { CommandSpinner } from "./command-spinner";
import { spinnerMessages } from "./spinner-messages";
import { createTheme } from "./theme";

// The command spinner is the async-command wiring (story 10-13): it ties a raw
// operation to a command's copy and the Spinner primitive, so `sync`, `rebuild`,
// `pull`, `truncate`, `install` and `update apply` get their status line for
// free. update-check (dynamic outcome) uses the Spinner directly instead. Tests
// drive it through ink's renderer with a fake operation.

const theme = createTheme();

// The spinner settles asynchronously (microtask resolve → setState → ink
// re-render), so poll the frame until it shows what we're waiting for rather
// than sleeping a fixed time that races the flush under parallel load.
async function frameShowing(
  lastFrame: () => string | undefined,
  needle: string,
): Promise<string> {
  for (let i = 0; i < 200; i++) {
    if ((lastFrame() ?? "").includes(needle)) break;
    await new Promise((r) => setTimeout(r, 5));
  }
  return lastFrame() ?? "";
}

test("on a successful operation it shows the command's success line", async () => {
  const { lastFrame } = render(
    <CommandSpinner
      copy={spinnerMessages.sync("my-workspace")}
      operation={() => Promise.resolve()}
      nerdfont={false}
      theme={theme}
    />,
  );

  const frame = await frameShowing(lastFrame, "✓");
  expect(frame).toContain("✓");
  expect(frame).toContain("my-workspace synchronized!");
});

test("on a failing operation it shows the command's summarized failure line", async () => {
  let ok: boolean | undefined;
  const { lastFrame } = render(
    <CommandSpinner
      copy={spinnerMessages.sync("my-workspace")}
      operation={() => Promise.reject(new Error("remote down\n  at push"))}
      nerdfont={false}
      theme={theme}
      onDone={(o) => (ok = o)}
    />,
  );

  const frame = await frameShowing(lastFrame, "𐄂");
  expect(frame).toContain("𐄂");
  // The command copy wraps the summarized error (first line only).
  expect(frame).toContain("something went wrong during syncing. error: remote down");
  expect(frame).not.toContain("at push");
  expect(ok).toBe(false);
});
