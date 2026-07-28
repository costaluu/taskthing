import { test, expect } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import { createMdwal } from "./mdwal";
import { createManager } from "./manager";

// The manager seam is its public API against *real* git: a bare repo standing in
// for the remote, plus temp working directories standing in for peers. Tests
// assert on observable git and filesystem state — which branches exist, what a
// branch's files contain — never on the sequence of git commands issued.
const taskSchema = z.object({ title: z.string() });

async function tempDir(prefix: string): Promise<string> {
  return await mkdtemp(join(tmpdir(), prefix));
}

async function bareRemote(): Promise<string> {
  const path = await tempDir("remote-");
  await Bun.$`git init --bare --initial-branch=master ${path}`.quiet();
  return path;
}

/** Read one file from a branch of the remote, without checking anything out. */
async function showRemote(remote: string, ref: string, path: string): Promise<string> {
  return await Bun.$`git --git-dir=${remote} show ${ref}:${path}`.text();
}

async function remoteBranches(remote: string): Promise<string[]> {
  const format = "--format=%(refname:short)";
  const out = await Bun.$`git --git-dir=${remote} for-each-ref ${format} refs/heads`.text();
  return out.split("\n").filter((l) => l.length > 0).sort();
}

/** Every file tracked on a branch of the remote. */
async function remoteFiles(remote: string, ref: string): Promise<string[]> {
  const out = await Bun.$`git --git-dir=${remote} ls-tree -r --name-only ${ref}`.text();
  return out.split("\n").filter((l) => l.length > 0);
}

/**
 * Stand in for another peer's rebuild: put a consolidated entity on master.
 * Returns the entity's id.
 */
async function publishToMaster(
  remote: string,
  id: string,
  title: string,
  options: { ts?: bigint; thresholds?: Record<string, string> } = {},
): Promise<string> {
  const ts = options.ts ?? 500n;
  const clone = await tempDir("rebuilder-");
  await Bun.$`git clone --quiet --branch master ${remote} ${clone}`.quiet();
  await Bun.$`git -C ${clone} config user.name bob`.quiet();
  await Bun.$`git -C ${clone} config user.email bob@taskthing.local`.quiet();
  await Bun.write(
    join(clone, "tasks", `${id}.md`),
    `---\ncreatedAt: ${ts}\ntitle: ${JSON.stringify(title)}\ntitle_lastModified: ${ts}\ntitle_lastModifiedBy: "bob"\n---\n`,
  );
  if (options.thresholds) {
    await Bun.write(
      join(clone, ".taskthing", "thresholds.json"),
      JSON.stringify(options.thresholds),
    );
  }
  await Bun.$`git -C ${clone} add -A`.quiet();
  await Bun.$`git -C ${clone} commit -m ${"taskthing: consolidated snapshot"}`.quiet();
  await Bun.$`git -C ${clone} push origin master`.quiet();
  await rm(clone, { recursive: true, force: true });
  return id;
}

/** Stand in for another peer: publish a users/<user> branch carrying a log. */
async function publishPeerLog(
  remote: string,
  user: string,
  logLines: string[],
): Promise<void> {
  const clone = await tempDir("peer2-");
  await Bun.$`git clone --quiet --branch master ${remote} ${clone}`.quiet();
  await Bun.$`git -C ${clone} config user.name ${user}`.quiet();
  await Bun.$`git -C ${clone} config user.email ${`${user}@taskthing.local`}`.quiet();
  const branch = `users/${user}`;
  // Continue the peer's existing branch when they already have one — a peer who
  // purged publishes an *empty* log onto their own history.
  const existing = await Bun.$`git -C ${clone} rev-parse --verify --quiet ${`origin/${branch}`}`
    .quiet()
    .nothrow();
  if (existing.exitCode === 0) {
    await Bun.$`git -C ${clone} checkout --quiet -B ${branch} ${`origin/${branch}`}`.quiet();
  } else {
    await Bun.$`git -C ${clone} checkout --quiet -b ${branch}`.quiet();
  }
  const log = logLines.length > 0 ? logLines.join("\n") + "\n" : "";
  await Bun.write(join(clone, "events.log"), log);
  await Bun.$`git -C ${clone} add -f events.log`.quiet();
  await Bun.$`git -C ${clone} commit --allow-empty -m ${"taskthing: publish events"}`.quiet();
  await Bun.$`git -C ${clone} push origin ${branch}`.quiet();
  await rm(clone, { recursive: true, force: true });
}

function createLine(id: string, title: string, ts: bigint, author: string): string {
  const payload = JSON.stringify({ author, snapshot: { title } });
  return `${ts}::CREATE::task::${id}::${payload}`;
}

function updateLine(
  id: string,
  field: string,
  value: unknown,
  ts: bigint,
  author: string,
): string {
  const payload = JSON.stringify({ author, field, value });
  return `${ts}::UPDATE_FIELD::task::${id}::${payload}`;
}

async function remoteTip(remote: string, ref: string): Promise<string> {
  return (await Bun.$`git --git-dir=${remote} rev-parse ${ref}`.text()).trim();
}

test("sync publishes this peer's events and leaves master alone when it hasn't advanced", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({
      root,
      mdwal,
      author: () => "alice",
      clock: () => ts,
    });
    await manager.init(remote);
    const masterBefore = await remoteTip(remote, "master");

    // Work done after associating: two events in this peer's log.
    const task = await mdwal.createEntity("task", { title: "buy milk" });
    await mdwal.updateField("task", task.id, "title", "milk 2%");

    await manager.sync();

    // The peer's branch now carries the log, verbatim — the same two events,
    // in order.
    const published = await showRemote(remote, "users/alice", "events.log");
    expect(published.split("\n").filter((l) => l.length > 0)).toHaveLength(2);
    expect(published).toContain("CREATE");
    expect(published).toContain("UPDATE_FIELD");

    // Publishing is all that happened: master is a consolidated snapshot only a
    // rebuild may advance, and derived files are still never committed.
    expect(await remoteTip(remote, "master")).toBe(masterBefore);
    const tracked = await remoteFiles(remote, "users/alice");
    expect(tracked.some((f) => f.startsWith("tasks/"))).toBe(false);

    // Syncing again with nothing new is a no-op, not an error or an empty commit.
    const tipAfterFirst = await remoteTip(remote, "users/alice");
    await manager.sync();
    expect(await remoteTip(remote, "users/alice")).toBe(tipAfterFirst);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("pull re-derives local state from master without purging or pushing", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    // Someone else consolidated a task into master while we were away.
    const consolidated = await publishToMaster(remote, "task-from-master", "shared work");

    // ...and we have local work of our own, not yet in master.
    const mine = await mdwal.createEntity("task", { title: "my own task" });
    await manager.sync();
    const logBefore = await Bun.file(join(root, "events.log")).text();
    const branchTipBefore = await remoteTip(remote, "users/alice");

    await manager.pull();

    // Local state is master's snapshot *plus* our own events replayed over it —
    // neither side is lost.
    expect((await mdwal.read("task", consolidated)).title).toBe("shared work");
    expect((await mdwal.read("task", mine.id)).title).toBe("my own task");

    // pull is read-only: the log is untouched (no purge) and nothing was pushed.
    expect(await Bun.file(join(root, "events.log")).text()).toBe(logBefore);
    expect(await remoteTip(remote, "users/alice")).toBe(branchTipBefore);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("sync after master advanced purges consolidated events and re-derives", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    const mine = await mdwal.createEntity("task", { title: "buy milk" }); // ts 2000
    await manager.sync();

    // A rebuilder consolidated our event into master and published how far each
    // peer was folded in.
    await publishToMaster(remote, mine.id, "buy milk", {
      ts: 2000n,
      thresholds: { alice: "2000" },
    });

    // Work authored *after* the consolidation point.
    await mdwal.updateField("task", mine.id, "title", "milk 2%"); // ts 3000

    await manager.sync();

    // The consolidated event is gone from our log; the newer one survives —
    // safe by construction, since a monotonic log never appends below the
    // threshold (ADR-0006).
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.ts)).toEqual([3000n]);

    // Local state is master plus our surviving event, folded under LWW.
    expect((await mdwal.read("task", mine.id)).title).toBe("milk 2%");

    // The compacted log is published too, so our branch reflects the compaction.
    const published = await showRemote(remote, "users/alice", "events.log");
    expect(published).toBe(await Bun.file(join(root, "events.log")).text());
    expect(published.split("\n").filter((l) => l.length > 0)).toHaveLength(1);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("rebuild consolidates every peer's log into master and publishes the thresholds", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    const mine = await mdwal.createEntity("task", { title: "buy milk" }); // ts 2000

    // Another peer published work of their own while we had ours unsynced.
    await publishPeerLog(remote, "bob", [createLine("bob-1", "bob's task", 3000n, "bob")]);
    const bobTipBefore = await remoteTip(remote, "users/bob");

    await manager.rebuild();

    // Master now holds everyone's work, folded under LWW — including our own
    // events, which rebuild syncs before consolidating.
    expect(await showRemote(remote, "master", `tasks/${mine.id}.md`)).toContain("buy milk");
    expect(await showRemote(remote, "master", "tasks/bob-1.md")).toContain("bob's task");

    // It stays a pure consolidated snapshot: no events accumulate on master.
    expect(await showRemote(remote, "master", "events.log")).toBe("");

    // The threshold map says how far each peer was folded in, so each of them
    // knows exactly how much they may purge.
    expect(JSON.parse(await showRemote(remote, "master", ".taskthing/thresholds.json"))).toEqual({
      alice: "2000",
      bob: "3000",
    });

    // Consolidation only ever writes master — never anyone's working branch.
    expect(await remoteTip(remote, "users/bob")).toBe(bobTipBefore);

    // Rebuild closes with a normal sync, and it is that sync — using the
    // threshold just published — that purges the rebuilder's own branch.
    expect(await mdwal.parseLog()).toEqual([]);
    expect(await showRemote(remote, "users/alice", "events.log")).toBe("");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("rebuild folds over the existing master, so purged history is never lost", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    const task = await mdwal.createEntity("task", { title: "first" }); // ts 2000

    // Another peer's later edit wins the LWW race and gets consolidated.
    await publishPeerLog(remote, "bob", [
      updateLine(task.id, "title", "bob wins", 5000n, "bob"),
    ]);
    await manager.rebuild();
    expect(await showRemote(remote, "master", `tasks/${task.id}.md`)).toContain("bob wins");

    // Bob then syncs and purges: the winning event now exists *only* inside
    // master's snapshot, in no log at all.
    await publishPeerLog(remote, "bob", []);

    // A third peer shows up with an edit that is older than the consolidated
    // one — it lost the race, and consolidating again must not revive it.
    await publishPeerLog(remote, "carol", [
      updateLine(task.id, "title", "carol loses", 3000n, "carol"),
    ]);
    await manager.rebuild();

    // Master is cumulative: the fold happens *over* the consolidated snapshot,
    // so its LWW companions still outrank a stale event from a log.
    expect(await showRemote(remote, "master", `tasks/${task.id}.md`)).toContain("bob wins");
    expect((await mdwal.read("task", task.id)).title).toBe("bob wins");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("a rebuild whose push is rejected redoes the consolidation against the new master", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });

    // Someone else's rebuild lands between our collection and our push — once.
    let interleaved = 0;
    const manager = createManager({
      root,
      mdwal,
      author: () => "alice",
      clock: () => ts,
      beforeMasterPush: async () => {
        if (interleaved++ > 0) return;
        await publishToMaster(remote, "other-1", "concurrent work", {
          ts: 4000n,
          thresholds: { dave: "4000" },
        });
      },
    });
    await manager.init(remote);

    const mine = await mdwal.createEntity("task", { title: "buy milk" }); // ts 2000
    await manager.rebuild();

    // Our push was refused because master had moved, so we consolidated again
    // from scratch — and the winner's work is still there.
    expect(interleaved).toBe(2);
    expect(await showRemote(remote, "master", "tasks/other-1.md")).toContain("concurrent work");

    // ...alongside ours, which the retry folded over the master we lost to.
    expect(await showRemote(remote, "master", `tasks/${mine.id}.md`)).toContain("buy milk");

    // The threshold map covers both peers: neither is told to purge work that
    // this master doesn't actually contain.
    expect(JSON.parse(await showRemote(remote, "master", ".taskthing/thresholds.json"))).toEqual({
      alice: "2000",
      dave: "4000",
    });
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

async function commitCount(remote: string, ref: string): Promise<number> {
  const out = await Bun.$`git --git-dir=${remote} rev-list --count ${ref}`.text();
  return Number(out.trim());
}

test("truncate keeps the last N commits of a branch this peer owns", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    // A history worth bounding: master's initial snapshot, the association,
    // three publications, and a rebuild that consolidates them — so the older
    // commits are safe to drop.
    for (const title of ["a", "b", "c"]) {
      await mdwal.createEntity("task", { title });
      await manager.sync();
    }
    await manager.rebuild();
    expect(await commitCount(remote, "users/alice")).toBeGreaterThan(2);
    const logBefore = await showRemote(remote, "users/alice", "events.log");

    await manager.truncateHistory({ keepN: 2 });

    // History is bounded, but the current state is untouched: the tip still
    // carries the whole log.
    expect(await commitCount(remote, "users/alice")).toBe(2);
    expect(await showRemote(remote, "users/alice", "events.log")).toBe(logBefore);

    // Another peer's branch is not ours to rewrite.
    await publishPeerLog(remote, "bob", [createLine("bob-1", "bob's task", 3000n, "bob")]);
    const bobTip = await remoteTip(remote, "users/bob");
    await expect(
      manager.truncateHistory({ branch: "users/bob", keepN: 1 }),
    ).rejects.toThrow(/own/i);
    expect(await remoteTip(remote, "users/bob")).toBe(bobTip);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("truncate refuses to cut below the last consolidated point", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    await mdwal.createEntity("task", { title: "a" }); // ts 2000
    await manager.sync();
    await mdwal.createEntity("task", { title: "b" }); // ts 3000
    await manager.sync();
    const tipBefore = await remoteTip(remote, "users/alice");

    // Nothing has been consolidated yet, so no commit carrying events may be
    // dropped — no other peer has seen them via master.
    await expect(manager.truncateHistory({ keepN: 1 })).rejects.toThrow(/consolidat/i);
    expect(await remoteTip(remote, "users/alice")).toBe(tipBefore);

    // After a rebuild consolidates up to ts 3000, that history is safe to drop:
    // master carries it now.
    await manager.rebuild();
    await manager.truncateHistory({ keepN: 1 });
    expect(await commitCount(remote, "users/alice")).toBe(1);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("disassociate detaches from the remote, preserving both sides", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    let manager: ReturnType<typeof createManager>;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      // The workspace is remote exactly while it has a remote to publish to.
      mode: async () => ((await manager.isAssociated()) ? "remote" : "local"),
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);

    const task = await mdwal.createEntity("task", { title: "buy milk" });
    await manager.sync();
    const branchesBefore = await remoteBranches(remote);
    const aliceTip = await remoteTip(remote, "users/alice");

    await manager.disassociate();

    // The remote is untouched — other peers are unaffected and we could
    // associate again later.
    expect(await remoteBranches(remote)).toEqual(branchesBefore);
    expect(await remoteTip(remote, "users/alice")).toBe(aliceTip);

    // Local work survives the transition; nothing is lost by going local.
    expect((await mdwal.read("task", task.id)).title).toBe("buy milk");

    // From here it behaves as a local workspace: operations still write files
    // and their LWW metadata, but nothing is logged — there is nowhere to
    // publish a log to, and no peer to consolidate it.
    expect(await manager.isAssociated()).toBe(false);
    await mdwal.updateField("task", task.id, "title", "milk 2%");
    expect((await mdwal.read("task", task.id)).title).toBe("milk 2%");
    expect(await mdwal.parseLog()).toEqual([]);
    expect(await Bun.file(join(root, "events.log")).exists()).toBe(false);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("init keeps the peer's own events, even though master's log is empty", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });

    // A workspace that already carries authored history.
    const task = await mdwal.createEntity("task", { title: "buy milk" });
    await mdwal.updateField("task", task.id, "title", "milk 2%");

    await manager.init(remote);

    // master's log is empty because it is a consolidated snapshot — that must
    // not be achieved by destroying the peer's own log.
    expect(await showRemote(remote, "master", "events.log")).toBe("");
    expect((await mdwal.parseLog()).map((e) => e.ts)).toEqual([2000n, 3000n]);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

/** A peer whose workspace already has local work, not yet associated. */
async function localWorkspace(author: string, timestamps: () => bigint) {
  const root = await tempDir(`${author}-`);
  let manager: ReturnType<typeof createManager>;
  const mdwal = createMdwal({
    root,
    schemas: { task: taskSchema },
    mode: async () => ((await manager.isAssociated()) ? "remote" : "local"),
    clock: timestamps,
    author: () => author,
  });
  manager = createManager({ root, mdwal, author: () => author, clock: timestamps });
  return { root, mdwal, manager: manager! };
}

test("init joining an existing remote replays master and logs prior work retroactively", async () => {
  const remote = await bareRemote();
  let aliceTs = 1000n;
  const alice = await localWorkspace("alice", () => (aliceTs += 1000n));
  let bobTs = 100_000n;
  const bob = await localWorkspace("bob", () => (bobTs += 1000n));
  try {
    // Alice bootstraps the repo and consolidates a task into master.
    const hers = await alice.mdwal.createEntity("task", { title: "alice's task" });
    await alice.manager.init(remote);
    await alice.manager.rebuild();

    // Bob has work of his own, done before he ever heard of this remote.
    const his = await bob.mdwal.createEntity("task", { title: "bob's task" });

    await bob.manager.init(remote);

    // His prior work became part of the shared history — one retroactive CREATE
    // per local entity, under its existing id, stamped at the association.
    const events = await bob.mdwal.parseLog();
    expect(events).toHaveLength(1);
    expect(events[0]!.op).toBe("CREATE");
    expect(events[0]!.entityId).toBe(his.id);
    expect(events[0]!.payload.snapshot).toEqual({ title: "bob's task" });
    expect(events[0]!.ts).toBeGreaterThan(100_000n);

    // ...and it was published, so other peers can see it.
    expect(await showRemote(remote, "users/bob", "events.log")).toContain(his.id);

    // Joining also bootstrapped him: he now has everyone's consolidated work
    // alongside his own.
    expect((await bob.mdwal.read("task", hers.id)).title).toBe("alice's task");
    expect((await bob.mdwal.read("task", his.id)).title).toBe("bob's task");

    // master is untouched by a join — only a rebuild advances it.
    expect(await showRemote(remote, "master", "events.log")).toBe("");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(alice.root, { recursive: true, force: true });
    await rm(bob.root, { recursive: true, force: true });
  }
});

test("init refuses to reconcile a pre-existing branch with local work, unless confirmed", async () => {
  const remote = await bareRemote();
  let aliceTs = 1000n;
  const alice = await localWorkspace("alice", () => (aliceTs += 1000n));
  // Bob's machine number two: same identity, a branch already on the remote,
  // and unrelated local work sitting here.
  let bobTs = 100_000n;
  const bob = await localWorkspace("bob", () => (bobTs += 1000n));
  try {
    const hers = await alice.mdwal.createEntity("task", { title: "alice's task" });
    await alice.manager.init(remote);
    await alice.manager.rebuild();
    await publishPeerLog(remote, "bob", [
      createLine("bob-remote", "bob's published task", 50_000n, "bob"),
    ]);

    const local = await bob.mdwal.createEntity("task", { title: "bob's local task" });

    // Two histories that can't be reconciled: refusing is the only safe answer,
    // and there is no flag that makes this automatable.
    await expect(bob.manager.init(remote)).rejects.toThrow(/confirm/i);
    expect((await bob.mdwal.read("task", local.id)).title).toBe("bob's local task");

    // On an explicit confirmation, the remote history — the shared truth —
    // prevails, and the local-only work is gone for good. No backup.
    await bob.manager.init(remote, { confirmDestroy: true });
    expect(await bob.mdwal.readAll("task")).not.toContainEqual(
      expect.objectContaining({ id: local.id }),
    );

    // What he gets instead is the remote's version of his history, plus
    // everyone else's consolidated work.
    expect((await bob.mdwal.read("task", "bob-remote")).title).toBe("bob's published task");
    expect((await bob.mdwal.read("task", hers.id)).title).toBe("alice's task");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(alice.root, { recursive: true, force: true });
    await rm(bob.root, { recursive: true, force: true });
  }
});

test("master is stamped with the version that built it, and an older binary refuses to pull", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const newer = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
      version: "0.3.0",
    });
    const built = createManager({ root, mdwal: newer, author: () => "alice", clock: () => ts });
    await built.init(remote);
    await newer.createEntity("task", { title: "buy milk" });
    await built.rebuild();

    // master records the schema version it was consolidated at.
    expect(
      JSON.parse(await showRemote(remote, "master", ".taskthing/version.json")),
    ).toEqual({ version: "0.3.0" });

    // An older binary can't transform that data down to what it understands, so
    // it refuses rather than reading future-schema state best-effort (ADR-0007).
    const older = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
      version: "0.2.0",
    });
    const stale = createManager({ root, mdwal: older, author: () => "alice", clock: () => ts });
    await expect(stale.pull()).rejects.toThrow(/update/i);

    // A binary at or beyond master's version reads it normally.
    await built.pull();
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("rebuild refuses when any peer's log holds an event authored by a newer binary (ADR-0007)", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    let ts = 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (ts += 1000n),
      author: () => "alice",
      version: "0.2.0",
    });
    const manager = createManager({ root, mdwal, author: () => "alice", clock: () => ts });
    await manager.init(remote);
    await mdwal.createEntity("task", { title: "buy milk" }); // alice, at 0.2.0

    // A peer on a newer release published an event this binary has no schema for.
    const ahead = `3000::CREATE::task::bob-1::${JSON.stringify({
      author: "bob",
      snapshot: { title: "from the future" },
      version: "9.9.9",
    })}`;
    await publishPeerLog(remote, "bob", [ahead]);

    // Folding a future-schema event would bake a stale-schema master and corrupt
    // the newer values, so rebuild refuses outright rather than consolidating it.
    await expect(manager.rebuild()).rejects.toThrow(/9\.9\.9|update/i);

    // Master was never advanced: the future peer's entity is not consolidated.
    expect(await remoteFiles(remote, "master")).not.toContain("tasks/bob-1.md");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("association is read from the repository, so a fresh process sees it", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    const ts = () => 1000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: ts,
      author: () => "alice",
    });

    // Before any association there is no repository at all.
    const before = createManager({ root, mdwal, author: () => "alice", clock: ts });
    expect(await before.isAssociated()).toBe(false);

    await before.init(remote);

    // A manager built afterwards — as a later run of the binary would be — finds
    // the association by asking the repository, not by being told.
    const fresh = createManager({ root, mdwal, author: () => "alice", clock: ts });
    expect(await fresh.isAssociated()).toBe(true);

    await fresh.disassociate();
    expect(await createManager({ root, mdwal, author: () => "alice", clock: ts }).isAssociated())
      .toBe(false);
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("the confirmed destroy also replaces a local log left over from a past association", async () => {
  const remote = await bareRemote();
  let aliceTs = 1000n;
  const alice = await localWorkspace("alice", () => (aliceTs += 1000n));
  const root = await tempDir("bob-");
  try {
    await alice.manager.init(remote);
    await publishPeerLog(remote, "bob", [
      createLine("bob-remote", "bob's published task", 50_000n, "bob"),
    ]);

    // Bob's workspace still carries a log from an earlier association whose
    // repository is gone — a restored backup, a copied folder, a deleted .git.
    let bobTs = 100_000n;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => (bobTs += 1000n),
      author: () => "bob",
    });
    const manager = createManager({ root, mdwal, author: () => "bob", clock: () => bobTs });
    const local = await mdwal.createEntity("task", { title: "bob's local task" });
    expect(await mdwal.parseLog()).toHaveLength(1);

    await manager.init(remote, { confirmDestroy: true });

    // The remote history prevails whole: the local log goes with the local
    // files, replaced by the branch's own.
    expect((await mdwal.read("task", "bob-remote")).title).toBe("bob's published task");
    expect((await mdwal.parseLog()).map((e) => e.entityId)).toEqual(["bob-remote"]);
    expect(await mdwal.readAll("task")).not.toContainEqual(
      expect.objectContaining({ id: local.id }),
    );
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(alice.root, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});

test("init against an empty remote publishes master and the peer's working branch", async () => {
  const remote = await bareRemote();
  const root = await tempDir("peer-");
  try {
    // A workspace with real work already in it, not yet associated to anything.
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "local",
      clock: () => 1000n,
      author: () => "alice",
    });
    const task = await mdwal.createEntity("task", { title: "buy milk" });

    const manager = createManager({
      root,
      mdwal,
      author: () => "alice",
      clock: () => 2000n,
    });

    await manager.init(remote);

    // Both branches of the model exist: the consolidated snapshot and this
    // peer's working branch.
    expect(await remoteBranches(remote)).toEqual(["master", "users/alice"]);

    // master carries the derived snapshot — and an empty events.log, because it
    // is a consolidated state, never a place events accumulate.
    expect(await showRemote(remote, "master", `tasks/${task.id}.md`)).toContain("buy milk");
    expect(await showRemote(remote, "master", "events.log")).toBe("");
    expect(JSON.parse(await showRemote(remote, "master", ".taskthing/thresholds.json"))).toEqual({});

    // The working branch tracks only events; derived state is never committed
    // there, so it can't produce a YAML merge conflict (ADR-0003).
    expect(await showRemote(remote, "users/alice", ".gitignore")).toContain("tasks/");
    const tracked = await remoteFiles(remote, "users/alice");
    expect(tracked).toContain("events.log");
    expect(tracked.some((f) => f.startsWith("tasks/"))).toBe(false);

    // Locally the peer is left on their working branch, with their files intact.
    const branch = (await Bun.$`git -C ${root} rev-parse --abbrev-ref HEAD`.text()).trim();
    expect(branch).toBe("users/alice");
    expect((await mdwal.read("task", task.id)).title).toBe("buy milk");
  } finally {
    await rm(remote, { recursive: true, force: true });
    await rm(root, { recursive: true, force: true });
  }
});
