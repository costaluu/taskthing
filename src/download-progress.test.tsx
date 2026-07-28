import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { DownloadProgress } from "./download-progress";
import { createTheme } from "./theme";

// DownloadProgress is update apply's two-line status: a braille spinner beside
// the fixed pending message, and a sub-line that tracks the download (a live
// bar) or the swap + migrations that follow it ("applying update..."). It
// settles into the same single success/failure line Spinner draws.

const theme = createTheme();

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

test("before any report, it shows only the spinner frame and the pending message", () => {
  const never = () => new Promise<string>(() => {});

  const { lastFrame } = render(
    <DownloadProgress pending="updating taskthing..." run={never} nerdfont={false} theme={theme} />,
  );

  const frame = lastFrame() ?? "";
  expect(frame).toContain("updating taskthing...");
  expect(frame).toContain("⣾");
  expect(frame).not.toContain("downloading");
});

test("a downloading report with a known total draws a percentage bar", async () => {
  const { lastFrame } = render(
    <DownloadProgress
      pending="updating taskthing..."
      run={(report) => {
        report({ kind: "downloading", downloaded: 50, total: 100 });
        return new Promise<string>(() => {});
      }}
      nerdfont={false}
      theme={theme}
    />,
  );

  const frame = await frameShowing(lastFrame, "%");
  expect(frame).toContain("updating taskthing...");
  expect(frame).toContain("downloading new version...");
  expect(frame).toContain("50%");
  expect(frame).toContain("─");
});

test("a downloading report with no total falls back to a running byte count", async () => {
  const { lastFrame } = render(
    <DownloadProgress
      pending="updating taskthing..."
      run={(report) => {
        report({ kind: "downloading", downloaded: 2048, total: null });
        return new Promise<string>(() => {});
      }}
      nerdfont={false}
      theme={theme}
    />,
  );

  const frame = await frameShowing(lastFrame, "KB");
  expect(frame).toContain("2.0KB");
  expect(frame).toContain("downloading new version...");
});

test("an applying report swaps the sub-line to the applying message", async () => {
  const { lastFrame } = render(
    <DownloadProgress
      pending="updating taskthing..."
      run={(report) => {
        report({ kind: "applying" });
        return new Promise<string>(() => {});
      }}
      nerdfont={false}
      theme={theme}
    />,
  );

  const frame = await frameShowing(lastFrame, "applying update...");
  expect(frame).toContain("updating taskthing...");
  expect(frame).toContain("applying update...");
  expect(frame).not.toContain("downloading");
});

test("on success it settles into a single check-marked success line", async () => {
  let ok: boolean | undefined;
  const { lastFrame } = render(
    <DownloadProgress
      pending="updating taskthing..."
      run={async (report) => {
        report({ kind: "downloading", downloaded: 100, total: 100 });
        return "taskthing updated to version 1.2.3";
      }}
      nerdfont={false}
      theme={theme}
      onDone={(o) => (ok = o)}
    />,
  );

  const frame = await frameShowing(lastFrame, "✓");
  expect(frame).toContain("✓");
  expect(frame).toContain("taskthing updated to version 1.2.3");
  expect(frame).not.toContain("updating taskthing...");
  expect(ok).toBe(true);
});

test("on failure it settles into a cross-marked line with the error", async () => {
  let ok: boolean | undefined;
  const failureLine = "something went wrong during taskthing update. error: connection reset";
  const { lastFrame } = render(
    <DownloadProgress
      pending="updating taskthing..."
      run={async () => {
        throw new Error(failureLine);
      }}
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
