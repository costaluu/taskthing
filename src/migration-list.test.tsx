import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { MigrationList } from "./migration-list";
import { createTheme } from "./theme";

// The migration list draws the plain data Spec 0003's `migrations` produces
// (story 29-31): a "MIGRATIONS" title, each known migration numbered from the
// kv_store as `<version> <applied?>`, and a closing validation message telling
// the user whether the workspace is fully migrated.

const theme = createTheme();

function renderMigrations(rows: Parameters<typeof MigrationList>[0]["rows"]) {
  return render(<MigrationList rows={rows} nerdfont={false} theme={theme} />);
}

test("it draws the Migrations title and each version with its applied state", () => {
  const { lastFrame } = renderMigrations([
    { number: 1, version: "0.1.0", applied: true },
    { number: 2, version: "0.2.0", applied: false },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("MIGRATIONS");
  expect(frame).toContain("1.");
  expect(frame).toContain("0.1.0");
  expect(frame).toContain("yes");
  expect(frame).toContain("2.");
  expect(frame).toContain("0.2.0");
  expect(frame).toContain("no");
});

test("it closes with a validation message reflecting whether any is pending", () => {
  // All applied: an info line saying the workspace is fully migrated.
  const clean = renderMigrations([
    { number: 1, version: "0.1.0", applied: true },
  ]);
  const cleanFrame = clean.lastFrame() ?? "";
  expect(cleanFrame).toContain("ℹ️");
  expect(cleanFrame).toContain("this workspace has no migrations pending");

  // Something unapplied: a warning line to check the installation.
  const pending = renderMigrations([
    { number: 1, version: "0.1.0", applied: true },
    { number: 2, version: "0.2.0", applied: false },
  ]);
  const pendingFrame = pending.lastFrame() ?? "";
  expect(pendingFrame).toContain("⚠️");
  expect(pendingFrame).toContain(
    "there's pending migrations. verify your taskthing installation.",
  );
});

test("no known migrations at all still shows the Migrations title and the no-pending message", () => {
  const { lastFrame } = renderMigrations([]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("MIGRATIONS");
  expect(frame).toContain("ℹ️");
  expect(frame).toContain("this workspace has no migrations pending");
});
