import { mkdir, readdir } from "node:fs/promises";
import { join } from "node:path";

import type { Event, Mdwal } from "./mdwal";
import type { Config } from "./schema";

// ── migration runner (structural — ADR-0007) ─────────────────────────────────
//
// Applies the binary's embedded *structural* migrations to every workspace on
// the machine (Spec 0005 Seam 2). A structural migration is a static mdwal log
// of folder ops (create/rename/truncate/delete a fixed folder), bound to a
// version. The runner replays each pending migration with logging OFF — a change
// every peer applies identically from its own binary is not a user-authored LWW
// event — and records it so it never re-runs. Data migrations are NOT here: they
// are replay-time transforms in mdwal (Spec 0001).

/** An embedded structural migration: the version it ships at, and its log. */
export interface Migration {
  version: string;
  content: string;
}

/**
 * A version-keyed config rewrite. Config is global, outside the event/LWW domain
 * and never replayed from a log, so migrating a value is an explicit, author-
 * controlled code transform applied directly — mdwal never coerces on its own.
 */
export interface ConfigMigration {
  version: string;
  apply(config: Config): Config;
}

/** Read/write access to the machine's single config.md. */
export interface ConfigBoundary {
  read(): Promise<Config>;
  write(config: Config): Promise<void>;
}

export interface MigrationRunnerDeps {
  /** The binary's embedded set — the sole source of truth for what exists. */
  migrations: Migration[];
  /** Every workspace root on the machine (local and remote). */
  workspaces: string[];
  /** An mdwal bound to a workspace root. */
  engine: (root: string) => Mdwal;
  /** The machine's config.md, when there are config migrations to apply. */
  config?: ConfigBoundary;
  /** The taskthing config dir holding config.md and the global `migrations/` record. */
  configHome?: string;
  /** Version-keyed config rewrites, applied once per machine (not per workspace). */
  configMigrations?: ConfigMigration[];
}

export interface MigrationRunner {
  run(): Promise<void>;
}

/** Apply one parsed folder-op event, never logging it. */
async function applyEvent(mdwal: Mdwal, ev: Event): Promise<void> {
  const off = { log: false };
  switch (ev.op) {
    case "CREATE_FOLDER":
      await mdwal.createFolder(ev.entityType, off);
      return;
    case "RENAME_FOLDER":
      await mdwal.renameFolder(ev.entityType, ev.payload.to as string, off);
      return;
    case "TRUNCATE_FOLDER":
      await mdwal.truncateFolder(ev.entityType, off);
      return;
    case "DELETE_FOLDER":
      await mdwal.deleteFolder(ev.entityType, off);
      return;
    default:
      throw new Error(`structural migration carries a non-folder op: ${ev.op}`);
  }
}

export function createMigrationRunner(deps: MigrationRunnerDeps): MigrationRunner {
  // A workspace records which structural migrations it has applied here, under
  // the gitignored `.taskthing/` (ADR-0003) — never committed, decided per peer.
  const recordDir = (root: string) => join(root, ".taskthing", "migrations");

  /** The migration versions recorded as applied in a given record folder. */
  async function appliedIn(dir: string): Promise<Set<string>> {
    try {
      const files = await readdir(dir);
      return new Set(files.map((f) => f.replace(/\.md$/, "")));
    } catch {
      // No record folder yet: nothing has been applied here.
      return new Set();
    }
  }

  /** The versions already applied to this workspace. */
  const applied = (root: string) => appliedIn(recordDir(root));

  async function applyMigration(mdwal: Mdwal, migration: Migration): Promise<void> {
    for (const ev of mdwal.parseEvents(migration.content)) {
      await applyEvent(mdwal, ev);
    }
  }

  async function migrateWorkspace(root: string): Promise<void> {
    const done = await applied(root);
    // Only migrations absent from this workspace's records run, in version
    // order, so structural changes compose predictably (stories 24/25).
    const pending = deps.migrations
      .filter((m) => !done.has(m.version))
      .sort((a, b) => Bun.semver.order(a.version, b.version));

    const mdwal = deps.engine(root);
    for (const migration of pending) {
      await applyMigration(mdwal, migration);
      // Record only after a successful apply, so a failure leaves it pending.
      await mkdir(recordDir(root), { recursive: true });
      await Bun.write(join(recordDir(root), `${migration.version}.md`), migration.content);
    }

    // A fixed-folder change moves the ignore set. Only remote workspaces have a
    // .gitignore (state is committed for local ones); regenerate it in place
    // from the new fixed-folder set, never inventing one (ADR-0003).
    if (pending.length > 0) await regenerateGitignore(root, mdwal);
  }

  async function run(): Promise<void> {
    // A failure in one place must not stop the others (story 26/34): attempt
    // every workspace and the config, then surface all the failures together.
    const failures: { where: string; error: unknown }[] = [];
    for (const root of deps.workspaces) {
      try {
        await migrateWorkspace(root);
      } catch (error) {
        failures.push({ where: root, error });
      }
    }

    // Config migrations run once for the whole machine, not per workspace, and
    // are a separate dimension: a failing workspace must never skip them.
    try {
      await migrateConfig();
    } catch (error) {
      failures.push({ where: "config.md", error });
    }

    if (failures.length > 0) {
      const detail = failures
        .map((f) => `${f.where}: ${f.error instanceof Error ? f.error.message : String(f.error)}`)
        .join("; ");
      throw new Error(`migration failed on ${failures.length} target(s): ${detail}`);
    }
  }

  async function migrateConfig(): Promise<void> {
    const all = deps.configMigrations ?? [];
    if (all.length === 0 || deps.config === undefined || deps.configHome === undefined) return;

    // Config migrations are recorded once per machine, in a `migrations/` folder
    // beside config.md (the same ADR-0003 per-peer rule as workspace records).
    const dir = join(deps.configHome, "migrations");
    const done = await appliedIn(dir);
    const pending = all
      .filter((m) => !done.has(m.version))
      .sort((a, b) => Bun.semver.order(a.version, b.version));
    if (pending.length === 0) return;

    let config = await deps.config.read();
    for (const migration of pending) {
      config = migration.apply(config);
    }
    await deps.config.write(config);

    // Record only after the write succeeds, so a failure leaves them pending.
    await mkdir(dir, { recursive: true });
    for (const migration of pending) {
      await Bun.write(join(dir, `${migration.version}.md`), migration.version);
    }
  }

  async function regenerateGitignore(root: string, mdwal: Mdwal): Promise<void> {
    const path = join(root, ".gitignore");
    if (!(await Bun.file(path).exists())) return;
    const ignored = mdwal.folders().map((f) => `${f}/`).join("\n");
    await Bun.write(path, `${ignored}\n.taskthing/\n`);
  }

  return { run };
}
