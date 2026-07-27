import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { WorkspaceList } from "./workspace-list";
import { createTheme } from "./theme";

// `workspace list` (mehorias item 6) draws every workspace in the Selection UI's
// static shape: a "Workspaces" title and one selector/dot/label row each, with
// the current one shown as the selected row. Tests feed it fixtures and assert on
// the rendered frame.

const theme = createTheme();

function renderWorkspaces(workspaces: string[], current: string) {
  return render(
    <WorkspaceList workspaces={workspaces} current={current} nerdfont={false} theme={theme} />,
  );
}

test("it draws the Workspaces title and every workspace name", () => {
  const { lastFrame } = renderWorkspaces(["main", "work"], "main");

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Workspaces");
  expect(frame).toContain("main");
  expect(frame).toContain("work");
});

test("the current workspace is the selected row; the others are not", () => {
  const { lastFrame } = renderWorkspaces(["main", "work"], "work");
  const lines = (lastFrame() ?? "").split("\n");

  const currentLine = lines.find((l) => l.includes("work"))!;
  const otherLine = lines.find((l) => l.includes("main"))!;

  // The current row carries the filled dot and the selector glyph (● and →
  // without nerdfont); the other carries the empty dot and no selector.
  expect(currentLine).toContain("●");
  expect(currentLine).toContain("→");
  expect(otherLine).toContain("○");
  expect(otherLine).not.toContain("→");
});

test("an empty workspace list shows an empty-state message instead of nothing", () => {
  const { lastFrame } = renderWorkspaces([], "main");

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Workspaces");
  expect(frame).toContain("no workspaces here");
});
