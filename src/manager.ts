import { $ } from "bun";
import { mkdir, rm } from "node:fs/promises";
import { join } from "node:path";

import { compareSemver, type Event, type Mdwal } from "./mdwal";

// ── manager (git orchestration) ─────────────────────────────────────────────
//
// The only module allowed to invoke git. Every other layer stays git-agnostic.
// The branch model it maintains:
//   master        — the consolidated snapshot: derived entity files are committed
//                   (the ADR-0003 exception), events.log is always empty, and a
//                   threshold map records how far each peer has been consolidated.
//   users/<user>  — one branch per peer, tracking only that peer's events.log;
//                   entity folders are gitignored, since state is always replayed.

/** Where master carries the `{ user: ts_ns }` consolidation threshold map. */
export const THRESHOLDS_FILE = ".taskthing/thresholds.json";

/**
 * Where master records the binary semver it was consolidated at (ADR-0007). A
 * binary older than that can't transform the data down to a schema it knows.
 */
export const VERSION_FILE = ".taskthing/version.json";

/** Local-only record of the master commit this peer last re-derived from. */
const SEEN_MASTER_FILE = ".taskthing/seen-master";

export interface ManagerConfig {
  /** Absolute path to the workspace root. */
  root: string;
  /** The engine owning this workspace's files and events. */
  mdwal: Mdwal;
  /** Injectable author source (global git user.name in production). */
  author: () => string;
  /** The same per-peer monotonic clock mdwal uses (ADR-0006). */
  clock: () => bigint;
  /**
   * Runs just before a rebuild publishes master. A seam for interleaving a
   * concurrent rebuild, which is otherwise a race no test can stage reliably.
   */
  beforeMasterPush?: () => Promise<void>;
}

export interface Manager {
  /**
   * Associate this workspace with a remote. Three cases: an empty remote is
   * bootstrapped from this workspace; an existing remote without this peer's
   * branch is joined, with prior local work entering the shared history; an
   * existing remote *with* this peer's branch plus local work is
   * unreconcilable, and is refused unless the caller confirms destroying the
   * local side. There is deliberately no bypass flag for that confirmation.
   */
  init(remoteUrl: string, options?: { confirmDestroy?: boolean }): Promise<void>;
  /**
   * Publish this peer's events to `users/<me>`. While master hasn't advanced,
   * that is all it does — the cheap common case.
   */
  sync(): Promise<void>;
  /**
   * Re-derive local state from the latest master: master's consolidated
   * snapshot with this peer's own events replayed over it. Read-only — no
   * purge, no push — and the primitive sync composes.
   */
  pull(): Promise<void>;
  /**
   * Consolidate every peer's log into a new master snapshot and publish it with
   * an updated threshold map. Writes only master — never anyone's working
   * branch, not even this peer's; the closing sync is what purges our own.
   */
  rebuild(): Promise<void>;
  /**
   * Bound repository growth by keeping only the last N commits of a branch this
   * peer owns. The tip's content is unchanged — a working branch's log lives
   * entirely in its latest commit — so this drops history, never state.
   */
  truncateHistory(options: { branch?: string; keepN: number }): Promise<void>;
  /**
   * Detach from the remote and go back to purely local use. The remote's
   * content is left completely alone — other peers are unaffected, and this
   * peer could associate again later.
   */
  disassociate(): Promise<void>;
  /** Whether this workspace currently has a remote to publish to. */
  isAssociated(): Promise<boolean>;
}

export function createManager(config: ManagerConfig): Manager {
  const { root } = config;
  const git = (...args: string[]) => $`git -C ${root} ${args}`.quiet();

  const userBranch = () => `users/${config.author()}`;

  async function pull(): Promise<void> {
    await git("fetch", "origin", "+refs/heads/master:refs/remotes/origin/master");
    await assertCanReadMaster();

    // Master's derived files are copied out of the object database rather than
    // checked out, so the working branch's index never learns about them —
    // derived state must not become tracked here (ADR-0003).
    const derived = config.mdwal.folders().map((f) => `${f}/`);
    const listed = await $`git -C ${root} ls-tree -r --name-only origin/master`
      .quiet()
      .text();
    for (const path of listed.split("\n").filter((l) => l.length > 0)) {
      if (!derived.some((d) => path.startsWith(d))) continue;
      const blob = await $`git -C ${root} show ${`origin/master:${path}`}`.quiet().text();
      await Bun.write(join(root, path), blob);
    }

    // Master is the base; our own log is folded over it under LWW, without
    // being re-logged.
    const base = await config.mdwal.snapshot();
    const events = await config.mdwal.parseLog();
    await config.mdwal.materialize(config.mdwal.replay(base, events));
  }

  /**
   * The folders holding derived state come from the schema itself, so an entity
   * type added by a migration is ignored on the working branch for free.
   */
  async function writeGitignore(): Promise<void> {
    const ignored = config.mdwal.folders().map((f) => `${f}/`).join("\n");
    await Bun.write(join(root, ".gitignore"), `${ignored}\n.taskthing/\n`);
  }

  /** Add, commit and push events.log — with no empty commit when nothing changed. */
  async function publish(): Promise<void> {
    await git("add", "events.log");
    const staged = await $`git -C ${root} diff --cached --name-only`.quiet().text();
    if (staged.trim().length === 0) return;

    await git("commit", "-m", "taskthing: publish events");
    await git("push", "origin", userBranch());
  }

  /** How far each peer was consolidated by the rebuild that produced master. */
  async function thresholdsFromMaster(): Promise<Record<string, string>> {
    const json = await $`git -C ${root} show ${`origin/master:${THRESHOLDS_FILE}`}`
      .quiet()
      .text();
    return JSON.parse(json) as Record<string, string>;
  }

  /** How far *this* peer was consolidated — how much of our log we may purge. */
  async function thresholdFromMaster(): Promise<bigint | null> {
    const value = (await thresholdsFromMaster())[config.author()];
    return value === undefined ? null : BigInt(value);
  }

  async function sync(): Promise<void> {
    await git("fetch", "origin", "+refs/heads/master:refs/remotes/origin/master");
    const tip = (await $`git -C ${root} rev-parse origin/master`.quiet().text()).trim();
    const seen = await Bun.file(join(root, SEEN_MASTER_FILE))
      .text()
      .catch(() => "");

    // Master advanced: drop what has already been consolidated, then re-derive
    // from the new snapshot before publishing the compacted log.
    if (tip !== seen.trim()) {
      const threshold = await thresholdFromMaster();
      if (threshold !== null) await config.mdwal.purge(threshold);
      await pull();
      await Bun.write(join(root, SEEN_MASTER_FILE), tip);
    }

    await publish();
  }

  /** Hash content straight into the object database and stage it at `path`. */
  async function addBlob(
    withIndex: (...args: string[]) => { text(): Promise<string> },
    content: string,
    path: string,
  ): Promise<void> {
    const blob = (
      await $`echo ${content} | git -C ${root} hash-object -w --stdin`.quiet().text()
    ).trim();
    await withIndex("update-index", "--add", "--cacheinfo", `100644,${blob},${path}`).text();
  }

  /**
   * Refuse to read a master consolidated by a newer binary. Reading future-schema
   * data best-effort could silently corrupt what this binary cannot transform
   * down — the version barrier (ADR-0007).
   */
  async function assertCanReadMaster(): Promise<void> {
    const ours = config.mdwal.version();
    if (ours === undefined) return;

    const json = await $`git -C ${root} show ${`origin/master:${VERSION_FILE}`}`
      .quiet()
      .nothrow()
      .text();
    if (json.trim().length === 0) return;

    const theirs = (JSON.parse(json) as { version: string }).version;
    if (compareSemver(theirs, ours) > 0) {
      throw new Error(
        `master was consolidated by version ${theirs}; update taskthing to >= ${theirs}`,
      );
    }
  }

  /** The branches a remote already has, without cloning or fetching it. */
  async function remoteBranches(remoteUrl: string): Promise<string[]> {
    const listed = await $`git ls-remote --heads ${remoteUrl}`.quiet().text();
    return listed
      .split("\n")
      .filter((l) => l.length > 0)
      .map((l) => l.split("refs/heads/")[1]!);
  }

  /** Every peer's log, read straight out of git — nothing is checked out. */
  async function collectPeerLogs(): Promise<Map<string, Event[]>> {
    await git("fetch", "origin", "+refs/heads/users/*:refs/remotes/origin/users/*");
    const format = "--format=%(refname:short)";
    const refs = await $`git -C ${root} for-each-ref ${format} refs/remotes/origin/users`
      .quiet()
      .text();

    const logs = new Map<string, Event[]>();
    for (const ref of refs.split("\n").filter((l) => l.length > 0)) {
      const user = ref.slice("origin/users/".length);
      const text = await $`git -C ${root} show ${`${ref}:events.log`}`.quiet().text();
      logs.set(user, config.mdwal.parseEvents(text));
    }
    return logs;
  }

  /**
   * A commit may only be dropped once master has consolidated everything it
   * carries — otherwise history no other peer has seen would be destroyed.
   */
  async function assertAboveConsolidationFloor(
    branch: string,
    oldestKept: string,
  ): Promise<void> {
    await git("fetch", "origin", "+refs/heads/master:refs/remotes/origin/master");
    const consolidated = (await thresholdFromMaster()) ?? 0n;

    const dropped = await $`git -C ${root} rev-list ${`${oldestKept}^@`}`.quiet().text();
    for (const commit of dropped.split("\n").filter((l) => l.length > 0)) {
      const text = await $`git -C ${root} show ${`${commit}:events.log`}`
        .quiet()
        .nothrow()
        .text();
      const unconsolidated = config.mdwal
        .parseEvents(text)
        .some((ev) => ev.ts > consolidated);
      if (unconsolidated) {
        throw new Error(
          `refusing to truncate ${branch}: it would drop events not yet consolidated into master`,
        );
      }
    }
  }

  /**
   * Commit a tree built from the consolidated state to master, from a scratch
   * index: the working branch's own index is never touched, and the derived
   * files — gitignored here — still make it onto master, where they belong.
   */
  async function buildMasterCommit(
    thresholds: Record<string, string>,
  ): Promise<string> {
    const index = join(root, ".taskthing", "rebuild-index");
    await mkdir(join(root, ".taskthing"), { recursive: true });
    await rm(index, { force: true });
    const withIndex = (...args: string[]) =>
      $`git -C ${root} ${args}`.quiet().env({ ...process.env, GIT_INDEX_FILE: index });

    await withIndex("read-tree", "--empty");
    for (const folder of config.mdwal.folders()) {
      // An entity type with nothing stored yet simply has no folder on disk.
      await withIndex("add", "-f", folder).nothrow();
    }

    // master's events.log is always empty, and the local log must survive this —
    // so the blob is hashed straight into the object database, never written to
    // the workspace.
    const emptyBlob = (await $`git -C ${root} hash-object -w --stdin < /dev/null`.quiet().text()).trim();
    await withIndex("update-index", "--add", "--cacheinfo", `100644,${emptyBlob},events.log`);
    await addBlob(withIndex, JSON.stringify(thresholds), THRESHOLDS_FILE);

    // Stamp master with the version that consolidated it, so an older binary can
    // tell it is looking at data it cannot transform down (ADR-0007).
    const version = config.mdwal.version();
    if (version !== undefined) {
      await addBlob(withIndex, JSON.stringify({ version }), VERSION_FILE);
    }

    const tree = (await withIndex("write-tree").text()).trim();
    const commit = (
      await $`git -C ${root} commit-tree ${tree} -m ${"taskthing: consolidated snapshot"}`
        .quiet()
        .text()
    ).trim();

    await rm(index, { force: true });
    return commit;
  }

  /**
   * Publish a consolidated commit as master — non-fast-forward, since master is
   * replaced wholesale by a deterministic rebuild, but only if master is still
   * the commit we consolidated against. A blind force could roll master back
   * behind a threshold some peer already purged against, destroying those
   * events. Answers whether the push was accepted.
   */
  async function pushMaster(commit: string, expected: string): Promise<boolean> {
    await config.beforeMasterPush?.();
    const push = await git(
      "push",
      `--force-with-lease=refs/heads/master:${expected}`,
      "origin",
      `${commit}:refs/heads/master`,
    ).nothrow();
    return push.exitCode === 0;
  }

  // Association is observable state, never a flag we carry: the workspace is
  // remote exactly while its repository has a remote configured, and git is the
  // one who knows. Asked fresh each time, so a later run of the binary — or a
  // disassociation — needs nothing remembered.
  async function isAssociated(): Promise<boolean> {
    const result = await git("remote", "get-url", "origin").nothrow();
    return result.exitCode === 0;
  }

  return {
    pull,

    isAssociated,

    async disassociate() {
      await git("remote", "remove", "origin");

      // The log has nowhere to go and no one to consolidate it, while the
      // derived files already hold the whole state. Only the remote's content
      // is sacred here — it is left exactly as it was.
      await rm(join(root, "events.log"), { force: true });
    },

    async truncateHistory({ branch = userBranch(), keepN }) {
      // Rewriting history is only ever allowed on a branch this peer owns —
      // never on someone else's working branch.
      if (branch !== userBranch() && branch !== "master") {
        throw new Error(`refusing to truncate ${branch}: not a branch you own`);
      }
      await git("fetch", "origin", `+refs/heads/${branch}:refs/remotes/origin/${branch}`);

      const listed = await $`git -C ${root} rev-list --max-count=${keepN} ${`origin/${branch}`}`
        .quiet()
        .text();
      const keep = listed.split("\n").filter((l) => l.length > 0).reverse();
      await assertAboveConsolidationFloor(branch, keep[0]!);

      // Re-commit the surviving trees as a fresh chain: the oldest kept commit
      // becomes a root, so everything before it is unreachable and gone.
      let parent: string | null = null;
      for (const commit of keep) {
        const tree = (await $`git -C ${root} rev-parse ${`${commit}^{tree}`}`.quiet().text()).trim();
        const args: string[] = parent === null ? [] : ["-p", parent];
        const message = (
          await $`git -C ${root} log -1 --format=%s ${commit}`.quiet().text()
        ).trim();
        parent = (
          await $`git -C ${root} commit-tree ${tree} ${args} -m ${message}`.quiet().text()
        ).trim();
      }

      await git("push", "--force", "origin", `${parent}:refs/heads/${branch}`);
      if (branch === userBranch()) await git("reset", "--hard", parent!);
    },

    async rebuild() {
      // Publish our own latest events first, so we consolidate them too.
      await sync();

      // Consolidation is a read-collect-publish cycle against one master. If
      // master moves under us, everything we collected is stale, so the whole
      // cycle runs again from the collection step against the new one.
      for (;;) {
        await git("fetch", "origin", "+refs/heads/master:refs/remotes/origin/master");
        const against = (await $`git -C ${root} rev-parse origin/master`.quiet().text()).trim();

        const logs = await collectPeerLogs();
        logs.set(config.author(), await config.mdwal.parseLog());

        const all: Event[] = [];
        // Thresholds accumulate like the snapshot does: the map we publish keeps
        // what master already promised other peers, or a peer who purged against
        // it would find no record that their events were ever consolidated.
        const thresholds = await thresholdsFromMaster();
        for (const [user, events] of logs) {
          all.push(...events);
          for (const ev of events) {
            const seen = thresholds[user];
            if (seen === undefined || ev.ts > BigInt(seen)) thresholds[user] = ev.ts.toString();
          }
        }
        all.sort((a, b) => (a.ts < b.ts ? -1 : a.ts > b.ts ? 1 : 0));

        // Master is cumulative and the fold happens *over* it: peers purge their
        // consolidated events, so a snapshot's LWW companions are the only record
        // that a value ever won a race. Folding over nothing would let a stale
        // event from some peer's log revive a value master already superseded.
        await pull();
        const base = await config.mdwal.snapshot();
        await config.mdwal.materialize(config.mdwal.replay(base, all));

        const commit = await buildMasterCommit(thresholds);
        if (await pushMaster(commit, against)) break;
      }

      // Purging our own branch is a sync responsibility, using the threshold we
      // just published — rebuild has a single job.
      await sync();
    },

    sync,

    async init(remoteUrl, options = {}) {
      const author = config.author();
      const branches = await remoteBranches(remoteUrl);
      const hasLocalWork = (
        await Promise.all(config.mdwal.entityTypes().map((t) => config.mdwal.readAll(t)))
      ).some((entities) => entities.length > 0);

      // Our branch exists on the remote *and* we have local work here: two
      // histories that cannot be merged without losing one. Refuse until the
      // caller states, explicitly, that the local side may be destroyed. A
      // failed association leaves the workspace exactly as it was.
      const rejoining = branches.includes(userBranch());
      if (rejoining && hasLocalWork && !options.confirmDestroy) {
        throw new Error(
          `${userBranch()} already exists on the remote and this workspace has local work; ` +
            "confirm that the local tasks may be destroyed to continue",
        );
      }

      await git("init", "--initial-branch=master");
      await git("config", "user.name", author);
      await git("config", "user.email", `${author}@taskthing.local`);
      await git("remote", "add", "origin", remoteUrl);

      // An empty remote: this workspace bootstraps the repository. master is the
      // consolidated snapshot — the current derived files, no events, an empty
      // threshold map — built entirely in the object database, because the
      // peer's own events.log is authored history that must survive.
      if (!branches.includes("master")) {
        const master = await buildMasterCommit({});
        await git("push", "origin", `${master}:refs/heads/master`);
      }

      // The working branch tracks only events; derived folders are ignored, so
      // no state is ever committed where two peers could conflict over it.
      await writeGitignore();

      if (rejoining) {
        // The remote history is the shared truth: local entities are dropped
        // without a backup, and this peer's published log takes their place —
        // log included, or a leftover one from a past association would both
        // survive the destroy and block the checkout that replaces it.
        for (const folder of config.mdwal.folders()) {
          await rm(join(root, folder), { recursive: true, force: true });
        }
        await rm(join(root, "events.log"), { force: true });
        await git("fetch", "origin", `+refs/heads/${userBranch()}:refs/remotes/origin/${userBranch()}`);
        await git("checkout", "-b", userBranch(), `origin/${userBranch()}`);
      } else {
        // A workspace that was local until now has no log yet; from here it does.
        const log = Bun.file(join(root, "events.log"));
        if (!(await log.exists())) await Bun.write(log, "");
        await git("checkout", "-b", userBranch());
        await git("add", ".gitignore", "events.log");
        await git("commit", "-m", "taskthing: associate workspace");
      }

      // Joining a remote that already exists: prior local work would otherwise
      // be invisible to every other peer forever, so each existing entity enters
      // the shared history as a retroactive CREATE, stamped now. Only creations
      // are synthesized — never edits this peer never logged.
      if (branches.includes("master") && !rejoining) {
        for (const entityType of config.mdwal.entityTypes()) {
          for (const entity of await config.mdwal.readAll(entityType)) {
            await config.mdwal.logExisting(entityType, entity.id);
          }
        }
      }

      await git("push", "origin", userBranch());
      await sync();
    },
  };
}
