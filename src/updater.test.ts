import { test, expect } from "bun:test";

import { createUpdater, type Release, type UpdaterDeps, type UpdateState } from "./updater";

// The update orchestrator decides whether a newer binary exists and applies it.
// It is tested with everything injected — release source, clock, config — so no
// test hits GitHub or swaps a real binary (Spec 0005 Seam 1). Here: the check
// decision, driven by semver comparison.

const LINUX = { os: "linux", arch: "x64" };

function release(version: string): Release {
  return {
    version,
    assets: [
      { name: "taskthing-linux-x64", url: `https://x/${version}/linux` },
      { name: "taskthing-darwin-arm64", url: `https://x/${version}/mac` },
    ],
  };
}

/** An in-memory config store standing in for config.md. */
function configStore(initial: UpdateState) {
  let state = initial;
  return {
    store: {
      read: async () => state,
      write: async (s: UpdateState) => {
        state = s;
      },
    },
    current: () => state,
  };
}

const FRESH: UpdateState = {
  lastCheck: null,
  updateAvailable: false,
  target: null,
  autoUpdate: "confirm",
};

const TWELVE_H = 12 * 60 * 60 * 1000;
const NOW = 100 * TWELVE_H;

/** A release source that must not be consulted. */
const forbidden = {
  latest: async () => {
    throw new Error("check hit the network when it should have used the cache");
  },
};

test("check reports a pending update when the release is newer, by semver", async () => {
  const { store } = configStore(FRESH);
  const updater = createUpdater({
    currentVersion: "1.9.0",
    platform: LINUX,
    // 1.10.0 is newer than 1.9.0 — a naive string compare would get this wrong.
    releaseSource: { latest: async () => release("1.10.0") },
    clock: { now: () => 1_000 },
    config: store,
  });

  expect(await updater.check()).toEqual({
    status: "pending",
    current: "1.9.0",
    target: "1.10.0",
  });
});

test("check reports latest when the release is equal or older", async () => {
  const { store } = configStore(FRESH);
  const same = createUpdater({
    currentVersion: "2.0.0",
    platform: LINUX,
    releaseSource: { latest: async () => release("2.0.0") },
    clock: { now: () => 1_000 },
    config: store,
  });
  expect(await same.check()).toEqual({ status: "latest" });

  const { store: store2 } = configStore(FRESH);
  const ahead = createUpdater({
    currentVersion: "2.1.0",
    platform: LINUX,
    releaseSource: { latest: async () => release("2.0.0") },
    clock: { now: () => 1_000 },
    config: store2,
  });
  expect(await ahead.check()).toEqual({ status: "latest" });
});

test("check reuses the cached answer within 12h without hitting the release source", async () => {
  const { store } = configStore({
    lastCheck: NOW - (TWELVE_H - 1), // just under 12h old
    updateAvailable: true,
    target: "1.5.0",
    autoUpdate: "confirm",
  });
  const updater = createUpdater({
    currentVersion: "1.4.0",
    platform: LINUX,
    releaseSource: forbidden,
    clock: { now: () => NOW },
    config: store,
  });

  // The cache says a 1.5.0 update is pending; check answers from it, no fetch.
  expect(await updater.check()).toEqual({
    status: "pending",
    current: "1.4.0",
    target: "1.5.0",
  });
});

test("a stale or forced check refetches and re-persists the result", async () => {
  const { store, current } = configStore({
    lastCheck: NOW - (TWELVE_H + 1), // just over 12h old
    updateAvailable: false,
    target: null,
    autoUpdate: "confirm",
  });
  const updater = createUpdater({
    currentVersion: "1.4.0",
    platform: LINUX,
    releaseSource: { latest: async () => release("1.6.0") },
    clock: { now: () => NOW },
    config: store,
  });

  expect(await updater.check()).toEqual({
    status: "pending",
    current: "1.4.0",
    target: "1.6.0",
  });
  // The fresh answer and the check time are written back so it self-heals.
  expect(current().lastCheck).toBe(NOW);
  expect(current().updateAvailable).toBe(true);
  expect(current().target).toBe("1.6.0");

  // force ignores the cache even when fresh.
  const { store: freshStore } = configStore({
    lastCheck: NOW,
    updateAvailable: false,
    target: null,
    autoUpdate: "confirm",
  });
  const forced = createUpdater({
    currentVersion: "1.4.0",
    platform: LINUX,
    releaseSource: { latest: async () => release("1.6.0") },
    clock: { now: () => NOW },
    config: freshStore,
  });
  expect(await forced.check({ force: true })).toEqual({
    status: "pending",
    current: "1.4.0",
    target: "1.6.0",
  });
});

test("a pending update with no asset for this platform is a clean error", async () => {
  const { store } = configStore(FRESH);
  const updater = createUpdater({
    currentVersion: "1.0.0",
    // The release ships linux/darwin assets, but this machine is windows.
    platform: { os: "win32", arch: "x64" },
    releaseSource: { latest: async () => release("2.0.0") },
    clock: { now: () => NOW },
    config: store,
  });

  // Better a clear "no binary for your OS" than offering the wrong one.
  expect(updater.check()).rejects.toThrow(/win32/);
});

// A recording harness for apply: tracks what got downloaded, swapped, migrated.
function applyHarness(autoUpdate: "confirm" | "silent") {
  const calls = { downloaded: [] as string[], swapped: 0, migrated: 0 };
  const { store, current } = configStore({
    lastCheck: null,
    updateAvailable: false,
    target: null,
    autoUpdate,
  });
  const deps: UpdaterDeps = {
    currentVersion: "1.0.0",
    platform: LINUX,
    releaseSource: { latest: async () => release("2.0.0") },
    clock: { now: () => NOW },
    config: store,
    downloader: {
      download: async (url) => {
        calls.downloaded.push(url);
        return new Uint8Array([1, 2, 3]);
      },
    },
    fs: {
      swap: async () => {
        calls.swapped += 1;
      },
    },
    runMigrations: async () => {
      calls.migrated += 1;
    },
  };
  return { deps, calls, current };
}

test("apply in confirm mode does nothing when the confirmation is declined", async () => {
  const { deps, calls } = applyHarness("confirm");
  const updater = createUpdater(deps);

  const result = await updater.apply({ confirm: async () => false });

  expect(result).toEqual({ status: "declined" });
  // Nothing was downloaded, swapped, or migrated.
  expect(calls.downloaded).toEqual([]);
  expect(calls.swapped).toBe(0);
  expect(calls.migrated).toBe(0);
});

test("apply in confirm mode proceeds once confirmed, then runs migrations", async () => {
  const { deps, calls } = applyHarness("confirm");
  const updater = createUpdater(deps);

  const result = await updater.apply({ confirm: async () => true });

  expect(result).toEqual({ status: "updated", target: "2.0.0" });
  expect(calls.downloaded).toEqual(["https://x/2.0.0/linux"]);
  expect(calls.swapped).toBe(1);
  expect(calls.migrated).toBe(1);
});

test("apply in silent mode proceeds without asking", async () => {
  const { deps, calls } = applyHarness("silent");
  const updater = createUpdater(deps);

  let asked = false;
  const result = await updater.apply({
    confirm: async () => {
      asked = true;
      return true;
    },
  });

  expect(result).toEqual({ status: "updated", target: "2.0.0" });
  expect(asked).toBe(false);
  expect(calls.swapped).toBe(1);
});

test("a successful apply clears the pending update from the cache", async () => {
  // Otherwise the next check reuses the fresh-but-stale cache and reports the
  // version just installed as still pending.
  const { deps, current } = applyHarness("silent");
  const updater = createUpdater(deps);

  await updater.apply({ confirm: async () => true });

  expect(current()).toEqual({
    lastCheck: NOW,
    updateAvailable: false,
    target: null,
    autoUpdate: "silent",
  });
});

test("apply forwards download progress from the downloader to onProgress", async () => {
  const { deps } = applyHarness("silent");
  deps.downloader = {
    download: async (_url, onProgress) => {
      onProgress?.(1, 3);
      onProgress?.(3, 3);
      return new Uint8Array([1, 2, 3]);
    },
  };
  const updater = createUpdater(deps);

  const reports: { downloaded: number; total: number | null }[] = [];
  await updater.apply({
    confirm: async () => true,
    onProgress: (downloaded, total) => reports.push({ downloaded, total }),
  });

  expect(reports).toEqual([
    { downloaded: 1, total: 3 },
    { downloaded: 3, total: 3 },
  ]);
});

test("apply fires onApplying once the download finishes, before the swap", async () => {
  const { deps, calls } = applyHarness("silent");
  const updater = createUpdater(deps);

  const order: string[] = [];
  const originalSwap = deps.fs!.swap;
  deps.fs!.swap = async (bytes) => {
    order.push("swapped");
    await originalSwap(bytes);
  };

  await updater.apply({
    confirm: async () => true,
    onApplying: () => order.push("applying"),
  });

  expect(order).toEqual(["applying", "swapped"]);
  expect(calls.swapped).toBe(1);
});

test("a failed download leaves the old binary in place — no half-swap", async () => {
  const { deps, calls } = applyHarness("silent");
  // The download blows up mid-flight.
  deps.downloader = {
    download: async () => {
      throw new Error("connection reset");
    },
  };
  const updater = createUpdater(deps);

  expect(updater.apply({ confirm: async () => true })).rejects.toThrow(/connection reset/);
  // Give the rejection a tick, then assert nothing was swapped or migrated.
  await Promise.resolve();
  expect(calls.swapped).toBe(0);
  expect(calls.migrated).toBe(0);
});

test("apply reports up-to-date without downloading when already current", async () => {
  const { deps, calls } = applyHarness("silent");
  deps.currentVersion = "2.0.0"; // same as the release
  const updater = createUpdater(deps);

  expect(await updater.apply({ confirm: async () => true })).toEqual({ status: "up-to-date" });
  expect(calls.downloaded).toEqual([]);
  expect(calls.swapped).toBe(0);
});

test("apply sweeps a stale binary left by a prior swap even when already up to date", async () => {
  // Windows can't overwrite the running exe in place, so a swap renames the old
  // one aside; this stray file is swept on every apply call, not only ones that
  // end up applying something (ADR-0008).
  const { deps, calls } = applyHarness("silent");
  deps.currentVersion = "2.0.0"; // same as the release, so apply won't swap
  let swept = 0;
  deps.fs!.cleanupStale = async () => {
    swept += 1;
  };
  const updater = createUpdater(deps);

  await updater.apply({ confirm: async () => true });

  expect(swept).toBe(1);
  expect(calls.swapped).toBe(0);
});
