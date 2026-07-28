import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { Spinner } from "./spinner";
import { createTheme } from "./theme";

// The spinner gives async commands (install/update/sync/rebuild/pull/truncate) a
// live status (story 10-13). It is a generic primitive: it animates a braille
// frame next to a pending message, then resolves into a success line or a
// failure line with a summarized error. The per-command copy is fixed elsewhere;
// here the run() simply resolves to the success line or rejects.

const theme = createTheme();

// The spinner settles asynchronously: run() resolves in a microtask, setState
// schedules a React update, and ink flushes the re-render a tick later. A fixed
// sleep races that flush under parallel load, so instead poll the frame until it
// shows what we're waiting for (or give up after ~1s and let the assertion fail
// with the last frame it saw).
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

test("while the operation runs, it shows a frame and the pending message", () => {
  // A promise that never settles keeps the spinner in its running state.
  const never = () => new Promise<string>(() => {});

  const { lastFrame } = render(
    <Spinner pending="syncing my-workspace..." run={never} nerdfont={false} theme={theme} />,
  );

  const frame = lastFrame() ?? "";
  expect(frame).toContain("syncing my-workspace...");
  // The braille spinner starts on its first frame; it is the same with or
  // without nerdfont.
  expect(frame).toContain("⣾");
});

test("on success it settles into a check-marked success line", async () => {
  let ok: boolean | undefined;
  const { lastFrame } = render(
    <Spinner
      pending="syncing my-workspace..."
      run={() => Promise.resolve("my-workspace synchronized!")}
      nerdfont={false}
      theme={theme}
      onDone={(o) => (ok = o)}
    />,
  );

  const frame = await frameShowing(lastFrame, "✓");
  expect(frame).toContain("✓");
  expect(frame).toContain("my-workspace synchronized!");
  // The in-progress line is gone once it settles.
  expect(frame).not.toContain("syncing my-workspace...");
  expect(ok).toBe(true);
});

test("onDone fires only after the settled line is painted, never on the pending frame", async () => {
  // Regression (item 15): callers unmount on onDone, so if it fired in the same
  // tick as the state update the terminal would freeze on "⣾ …". The frame the
  // caller would keep must already be the success line, not the spinner.
  let frameAtDone: string | undefined;
  const { lastFrame } = render(
    <Spinner
      pending="looking for updates..."
      run={() => Promise.resolve("you're on the latest version!")}
      nerdfont={false}
      theme={theme}
      onDone={() => (frameAtDone = lastFrame())}
    />,
  );

  await frameShowing(lastFrame, "✓");
  expect(frameAtDone).toContain("✓");
  expect(frameAtDone).toContain("you're on the latest version!");
  expect(frameAtDone).not.toContain("looking for updates...");
});

test("on failure it settles into a cross-marked line with the error", async () => {
  let ok: boolean | undefined;
  const failureLine = "something went wrong during syncing. error: remote unreachable";
  const { lastFrame } = render(
    <Spinner
      pending="syncing my-workspace..."
      run={() => Promise.reject(new Error(failureLine))}
      nerdfont={false}
      theme={theme}
      onDone={(o) => (ok = o)}
    />,
  );

  const frame = await frameShowing(lastFrame, "𐄂");
  expect(frame).toContain("𐄂");
  expect(frame).toContain(failureLine);
  expect(ok).toBe(false);
});
