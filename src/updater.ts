// ── update orchestrator ──────────────────────────────────────────────────────
//
// Decides whether a newer binary exists for this OS and applies it, all through
// injected boundaries so nothing here touches GitHub or the real binary: a
// release source, a downloader, a filesystem/binary-swap, a clock, and the
// config cache. Version comparison is semver (via Bun.semver), never string
// compare, so `1.10.0` is correctly newer than `1.9.0` (Spec 0005 Seam 1).

/** A GitHub release: its version and the per-OS/arch binary assets. */
export interface Release {
  version: string;
  assets: { name: string; url: string }[];
}

export interface ReleaseSource {
  latest(): Promise<Release>;
}

export interface Platform {
  os: string;
  arch: string;
}

/** The binary settings cached in config.md (Spec 0005 story 18). */
export interface UpdateState {
  /** Epoch ms of the last check, or null if never checked. */
  lastCheck: number | null;
  updateAvailable: boolean;
  /** The newer version if one is pending, else null. */
  target: string | null;
  /** Whether apply asks first (default) or updates unattended. */
  autoUpdate: "confirm" | "silent";
}

export interface ConfigStore {
  read(): Promise<UpdateState>;
  write(state: UpdateState): Promise<void>;
}

export interface Clock {
  now(): number;
}

/** Fetches an asset's bytes from its URL. */
export interface Downloader {
  download(url: string): Promise<Uint8Array>;
}

/** Replaces the running binary — download-then-replace, so a failure is safe. */
export interface BinaryFs {
  swap(bytes: Uint8Array): Promise<void>;
}

export type CheckResult =
  | { status: "latest" }
  | { status: "pending"; current: string; target: string };

export type ApplyResult =
  | { status: "up-to-date" }
  | { status: "declined" }
  | { status: "updated"; target: string };

export interface UpdaterDeps {
  currentVersion: string;
  platform: Platform;
  releaseSource: ReleaseSource;
  clock: Clock;
  config: ConfigStore;
  // Only apply needs these; check works without them.
  downloader?: Downloader;
  fs?: BinaryFs;
  /** Runs pending migrations across every workspace after the swap (story 15). */
  runMigrations?: () => Promise<void>;
}

export interface Updater {
  check(options?: { force?: boolean }): Promise<CheckResult>;
  apply(options: { confirm: () => Promise<boolean> }): Promise<ApplyResult>;
}

const TWELVE_H = 12 * 60 * 60 * 1000;

/**
 * The release asset for this platform: the one whose name carries both the OS
 * and the arch. Missing → a clean error, never the wrong binary (story 11).
 */
function selectAsset(release: Release, platform: Platform): { name: string; url: string } {
  const asset = release.assets.find(
    (a) => a.name.includes(platform.os) && a.name.includes(platform.arch),
  );
  if (asset === undefined) {
    throw new Error(`no release asset for ${platform.os}/${platform.arch} in ${release.version}`);
  }
  return asset;
}

export function createUpdater(deps: UpdaterDeps): Updater {
  const resultFor = (target: string | null): CheckResult =>
    target !== null
      ? { status: "pending", current: deps.currentVersion, target }
      : { status: "latest" };

  async function check(options: { force?: boolean } = {}): Promise<CheckResult> {
    const state = await deps.config.read();

    // Within 12h the saved answer stands, so repeat checks are cheap and work
    // offline (story 8); a forced or stale check refetches (story 9).
    const fresh = state.lastCheck !== null && deps.clock.now() - state.lastCheck < TWELVE_H;
    if (!options.force && fresh) {
      return resultFor(state.updateAvailable ? state.target : null);
    }

    const release = await deps.releaseSource.latest();
    const pending = Bun.semver.order(release.version, deps.currentVersion) > 0;
    // A pending update must have a binary to offer for this platform, or it is
    // no use pretending one is available.
    if (pending) selectAsset(release, deps.platform);
    const target = pending ? release.version : null;

    // Refresh the cache so staleness self-heals.
    await deps.config.write({
      ...state,
      lastCheck: deps.clock.now(),
      updateAvailable: pending,
      target,
    });
    return resultFor(target);
  }

  async function apply(options: { confirm: () => Promise<boolean> }): Promise<ApplyResult> {
    const result = await check();
    if (result.status === "latest") return { status: "up-to-date" };

    // An update may trigger an irreversible migration, so ask first unless the
    // user opted into silent auto-update (stories 13/14/19).
    const { autoUpdate } = await deps.config.read();
    if (autoUpdate === "confirm" && !(await options.confirm())) {
      return { status: "declined" };
    }

    if (deps.downloader === undefined || deps.fs === undefined) {
      throw new Error("apply needs a downloader and a filesystem boundary");
    }
    const asset = selectAsset(await deps.releaseSource.latest(), deps.platform);
    const bytes = await deps.downloader.download(asset.url);
    // Download first, then replace, so a failed download leaves the old binary
    // intact (story 16).
    await deps.fs.swap(bytes);
    // The data structure must match the new binary before it is used (story 15).
    await deps.runMigrations?.();

    return { status: "updated", target: result.target };
  }

  return { check, apply };
}
