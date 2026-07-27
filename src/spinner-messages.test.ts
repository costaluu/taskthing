import { test, expect } from "bun:test";

import { spinnerMessages } from "./spinner-messages";

// The spinner copy is fixed verbatim by CONTEXT (interfaces (TUI) §1),
// parameterized by workspace/branch/version. This module is the one source of
// that wording; the Spinner primitive just draws the strings it returns. The
// expected strings below are copied from CONTEXT — an independent source of
// truth, not recomputed.

test("workspace commands carry CONTEXT's copy, parameterized by name", () => {
  const sync = spinnerMessages.sync("my-workspace");
  expect(sync.pending).toBe("syncing my-workspace...");
  expect(sync.success).toBe("my-workspace synchronized!");
  expect(sync.failure(new Error("boom"))).toBe(
    "something went wrong during syncing. error: boom",
  );

  const rebuild = spinnerMessages.rebuild("my-workspace");
  expect(rebuild.pending).toBe("rebuilding my-workspace...");
  expect(rebuild.success).toBe("my-workspace rebuilt!");
  expect(rebuild.failure(new Error("boom"))).toBe(
    "something went wrong during rebuild. error: boom",
  );

  const pull = spinnerMessages.pull("my-workspace");
  expect(pull.pending).toBe("pulling my-workspace...");
  expect(pull.success).toBe("my-workspace pulled!");
  expect(pull.failure(new Error("boom"))).toBe(
    "something went wrong during pull. error: boom",
  );

  const truncate = spinnerMessages.truncate("my-workspace", "users/alice");
  expect(truncate.pending).toBe("truncating branch users/alice from my-workspace...");
  expect(truncate.success).toBe("my-workspace truncated!");
  expect(truncate.failure(new Error("boom"))).toBe(
    "something went wrong during truncating. error: boom",
  );
});

test("install and update carry CONTEXT's copy, including update-check's three outcomes", () => {
  const install = spinnerMessages.install;
  expect(install.pending).toBe("installing taskthing...");
  expect(install.success).toBe("taskthing installed!");
  expect(install.failure(new Error("boom"))).toBe(
    "something went wrong during taskthing installation. error: boom",
  );

  // update check has three outcomes: latest, a pending update, or an error.
  const check = spinnerMessages.updateCheck;
  expect(check.pending).toBe("looking for updates...");
  expect(check.latest).toBe("you're on the latest version!");
  expect(check.update("1.2.0", "1.3.0")).toBe("there's a pending update 1.2.0 → 1.3.0!");
  expect(check.failure(new Error("boom"))).toBe(
    "something went wrong during update check. error: boom",
  );

  // update apply names the version it reached.
  const apply = spinnerMessages.updateApply;
  expect(apply.pending).toBe("updating taskthing...");
  expect(apply.success("1.3.0")).toBe("taskthing updated to version 1.3.0");
  expect(apply.failure(new Error("boom"))).toBe(
    "something went wrong during taskthing update. error: boom",
  );
});

test("a failure summarizes the error to its first line, never a raw stack", () => {
  // Story 13: the failure line shows a legible summary, not a stack trace.
  const sync = spinnerMessages.sync("w");
  const line = sync.failure(new Error("remote unreachable\n    at push (git.ts:12)"));
  expect(line).toBe("something went wrong during syncing. error: remote unreachable");
});
