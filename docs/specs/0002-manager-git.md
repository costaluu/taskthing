# Spec 0002 — manager (git orchestration: init, sync, rebuild, pull, truncate)

> **Scope note.** This spec covers the **manager** — the layer that maps a workspace onto a
> git remote and keeps peers eventually consistent. It builds directly on the mdwal primitives
> frozen in [Spec 0001](./0001-mdwal-core.md) (`createEntity`, `updateField`, `parseLog`,
> `purge`, `replay`, read/serialize) and does not redefine them. The porcelain CLI wrappers,
> their spinners/confirmation UIs (ink TUI), the `kv_store`, `config.md` schema, auto-rebuild
> policy, and distribution/auto-update are **out of scope** — later specs.
>
> Source of truth for the domain: [`CONTEXT.md`](../../CONTEXT.md) (`manager` section) and
> [`docs/adr/`](../adr). This spec must not contradict them; where it restates a decision it
> does so to make the manager contract implementable. Especially load-bearing here:
> **ADR-0003** (remote workspaces version events only; `master` is the derived-snapshot
> exception) and **ADR-0006** (per-peer monotonic timestamps make timestamp-purge safe).

## Problem Statement

mdwal (Spec 0001) can record a single peer's changes as an append-only, LWW-resolved event log
inside one workspace folder. But a peer's log lives only on their machine. For several people to
actually share a workspace, those per-peer logs have to travel over a **plain git remote** — with
no central server — and be combined into one consistent state that any peer can bootstrap from and
converge to.

Doing that over git raises problems mdwal alone does not solve:

- **Where do events live in the repo?** If entity files (`tasks/`, `boards/`) were committed, two
  peers editing concurrently would produce a YAML merge conflict — the exact thing mdwal exists to
  avoid (ADR-0003).
- **How does a new peer bootstrap** from an existing repo without replaying everyone's entire
  history from the beginning of time?
- **How do peers reclaim space** — drop events that have already been folded into a shared
  consistent state — without ever deleting an event that some other peer hasn't seen yet?
- **How does a peer associate** an existing local workspace (with real work already in it) to a
  remote, or associate to a remote that already has their branch, without silently losing data?

Without a manager, mdwal is a single-player engine.

## Solution

Build the **manager** — the plumber layer that owns all git interaction for a remote workspace,
using the system `git` CLI (taskthing already hard-depends on git being installed, per ADR-0002).
It realizes a fixed branch model and five primitive operations.

**Branch model.** A remote workspace's git repository has:

- `master` — a single consolidated snapshot. **Uniquely** among branches, its derived entity files
  (`tasks/*.md`, `boards/*.md`, …) *are* committed, because it is only ever rewritten
  deterministically by `rebuild`, never hand-edited, so it can't suffer merge conflicts (ADR-0003).
  Its own `events.log` is **always empty**. It also carries the **consolidation threshold map**
  `{ "<user>": <ts_ns>, … }` recording, per peer, the greatest event timestamp folded into this
  snapshot.
- `users/<user>` — one branch per peer, tracking **only** that peer's `events.log`. Entity folders
  are `.gitignore`d (ADR-0003); state is always reconstructed by replay, never committed here.

**Five primitives:**

1. **`init`** — associate the current workspace with a remote URL, handling three cases (empty
   remote / existing remote without this user's branch / existing remote with this user's branch).
2. **`sync`** — publish this peer's events and, when `master` has advanced, purge already-consolidated
   events and re-derive local state.
3. **`pull`** — the read-only primitive: fetch/checkout `master` and re-derive local state by replay;
   no purge, no push (git-fetch/pull without push). `sync` composes it.
4. **`rebuild`** — collect every peer's log in memory, fold them into a new consolidated `master`
   snapshot, and publish it with an updated threshold map.
5. **`truncate`** — bound repository growth by keeping the last N commits of a branch the caller owns.

Each primitive is exposed as a plumber operation callable directly and composable by porcelain.

## User Stories

Actors: **peer** (a person on one machine), **new peer** (associating to an existing remote),
**rebuilder** (any peer running `rebuild`), **consuming layer** (porcelain `sync`/`rebuild`/CLI).

### Branch model & remote association (init)

1. As a peer with an empty remote, I want `init` to create `master` from my current workspace's
   consistent state (derived files, **without** any events) and a `users/<me>` branch to work on, so
   that a fresh repo is bootstrapped and I have somewhere to publish.
2. As a peer, I want `master`'s `events.log` to always be empty, so that `master` stays a pure
   consolidated snapshot and never a place events accumulate.
3. As a new peer with local pre-existing work and a remote that lacks my branch, I want `init` to
   preserve my local content by retroactively emitting a `CREATE` event (full snapshot) for each
   existing local entity, timestamped at the association moment, into a freshly created
   `users/<me>` `events.log`, so that my prior work becomes part of the shared history instead of
   being invisible to other peers forever.
4. As a new peer joining an existing remote (case 3 above), I want those retroactive `CREATE`s to be
   the *only* events synthesized (no field events), so that association is a clean set of creations,
   not a replay of edits I never logged.
5. As a peer whose branch already exists on the remote *and* who also has local pre-existing work, I
   want `init` to refuse to silently reconcile the two histories, so that I can't lose data by
   accident.
6. As that same peer, I want `init` (case 3) to require an explicit interactive confirmation with
   **no bypass flag**, so that destroying local work is always a deliberate, unautomatable choice.
7. As that same peer, on confirming, I want my local `tasks/`/`boards/` permanently replaced by a
   checkout of the existing remote branch (no backup), so that the remote history — the shared truth —
   prevails unambiguously.
8. As a new peer, I want `init` to check out `master` and re-derive local state so I immediately have
   everyone's consolidated tasks/boards, so that joining a repo is a one-command bootstrap.
9. As a peer, I want the `users/<user>` branch to `.gitignore` the entity folders and track only
   `events.log`, so that no derived state is ever committed to a working branch (ADR-0003).
10. As a peer, I want `init` to require a configured global git identity, so that authored events are
    attributable from the first association (ADR-0002); a missing identity is a hard error.

### Remote disassociation

11. As a peer, I want to disassociate a workspace from its remote at any time, so that I can go back
    to purely local use.
12. As a peer disassociating, I want the **remote content left untouched** (nothing deleted on the
    remote), so that other peers are unaffected and I could re-associate later.
13. As a peer disassociating, I want my local workspace to keep its current derived files and simply
    stop being event-logged (revert to local-workspace behavior per Spec 0001), so that I lose no
    tasks in the transition.

### Publishing & re-deriving (sync / pull)

14. As a peer, when `master` has *not* advanced since my last sync, I want `sync` to just add, commit,
    and push my `events.log` to `users/<me>` with a commit message, so that publishing is cheap in the
    common case.
15. As a peer, when `master` *has* advanced (a new consolidated snapshot exists), I want `sync` to
    purge my own events with `ts <= threshold[me]` (the value from `master`'s threshold map), so that
    already-consolidated events stop taking up space.
16. As a peer, I want that purge to be safe by construction — because my log is monotonic (ADR-0006),
    every event I appended after the last rebuild has `ts > threshold[me]` — so that an unconsolidated
    event is never purged.
17. As a peer, after purging in the advanced case, I want `sync` to check out the new `master` state
    and re-apply (replay) my remaining, newer-than-threshold events over it **without re-logging them**,
    so that my local state reflects everyone's consolidated work plus my own un-consolidated edits.
18. As a peer, I want `sync` in the advanced case to also push my (now purged) `events.log`, so that my
    branch on the remote reflects the compaction too.
19. As a power user, I want `pull` available as a standalone primitive that only checks out `master`
    and replays my local state — no purge, no push — so that I can force a re-derive from the latest
    `master` without a full sync.
20. As a consuming layer, I want `sync` to be built by *composing* `pull` with the purge step in the
    advanced case, so that the "download master + replay" logic exists in exactly one place.
21. As a peer, I want `sync` to be safe to run repeatedly on a schedule, so that porcelain can keep a
    workspace current in the background (the porcelain scheduling itself is out of scope here).

### Consolidation (rebuild)

22. As any peer, I want to run `rebuild` at any time (no special role required), so that consolidation
    is not gated on a coordinator.
23. As a rebuilder, I want `rebuild` to first run a `sync`, so that my own latest events are published
    before I consolidate.
24. As a rebuilder, I want every peer's log collected and combined **entirely in memory** — checking
    out / parsing each `users/<user>` branch for reading only — so that I never write to anyone else's
    branch during consolidation.
25. As a rebuilder, I want all collected events ordered (by timestamp) and folded via mdwal `replay`
    into a consolidated `master` snapshot, so that `master` reflects LWW across every peer.
26. As a rebuilder, I want to record, per peer, the greatest event timestamp I consolidated, published
    as `master`'s threshold map `{ "<user>": <ts_ns>, … }`, so that every peer knows exactly how far
    they may safely purge.
27. As a rebuilder, I want to commit the consolidated derived state to `master` (its `events.log` still
    empty), so that new peers can bootstrap from it and existing peers can re-derive from it.
28. As a rebuilder, I want the push to `master` to be **always non-fast-forward**, and, if rejected
    (another peer rebuilt concurrently), to redo the entire rebuild from the collection step against the
    new `master`, so that two concurrent rebuilds can never clobber each other.
29. As a rebuilder, I want `rebuild` to *not* rewrite any `users/<user>` branch directly — not even my
    own — so that consolidation only ever writes `master`.
30. As a rebuilder, I want `rebuild` to finish by returning to my own working branch and running a
    normal `sync`; it is that sync (using the threshold I just published) that purges my own branch, so
    that purge stays a `sync` responsibility and rebuild has a single job.
31. As a consuming layer, I want `rebuild` exposed as a callable primitive, so that an auto-rebuild
    policy (after X syncs) can be layered on later (the policy/config is out of scope here).

### Bounding growth (truncate history)

32. As a peer, I want `truncate` to keep only the last N commits of a branch, so that the repository
    doesn't grow without bound.
33. As a peer, I want to truncate only a branch I own — my own `users/<me>` or `master` — and never a
    third party's `users/<other>`, so that I can't rewrite someone else's history.
34. As a peer, I want `truncate` to refuse to cut **below the last consolidated point known from a
    rebuild**, so that I can never destroy events that no other peer has yet seen via `master`.
35. As a peer, I want `truncate` to accept an optional branch selector (defaulting per config), so that
    the common case (`master` or my branch) needs no argument.

### Cross-cutting

36. As a peer, I want every manager primitive to read the target branch/remote from `config.md` when
    not given explicitly, so that day-to-day commands need no flags (the config keys themselves are
    defined in a later spec; the manager only *consumes* them).
37. As a peer, I want manager operations to surface a clear error on any git failure (auth, network,
    rejected push, dirty tree), so that the porcelain layer can render a precise failure message.
38. As a consuming layer, I want the manager to be the *only* module that shells out to git, so that
    every other layer stays git-agnostic and testable without a repo.
39. As a peer, I want all timestamps the manager relies on (retroactive `CREATE`s, thresholds) to come
    from the same per-peer monotonic clock as mdwal (ADR-0006), so that purge/threshold reasoning holds.
40. As a new peer, I want `init` against an existing remote to fail cleanly (not partially associate) if
    the remote is unreachable or malformed, so that a failed association leaves my local workspace intact.

## Implementation Decisions

### Modules built

- **`manager`** — the git-orchestration module and the *only* module allowed to invoke git (via the
  system `git` CLI, shelled through `Bun.$`, consistent with the existing ADR-0002 git dependency).
  Public surface (names indicative, semantics normative):
  - `init(remoteUrl, { confirmDestroy })` — associate; dispatches to case 1.1 / 1.2 / 1.3.
  - `sync()` — publish + (when `master` advanced) purge + re-derive.
  - `pull()` — checkout `master` + replay local state; no purge, no push.
  - `rebuild()` — consolidate all peers into a new `master`.
  - `truncateHistory({ branch?, keepN })` — keep last N commits of an owned branch.
  - `disassociate()` — detach from remote, preserve remote content, revert workspace to local.
- Reuses, without redefining, the Spec 0001 mdwal primitives: `createEntity` (retroactive `CREATE`s in
  init 1.2), `parseLog` (reading every peer's log during rebuild), `purge(threshold)` (sync 2.2 and the
  rebuilder's closing sync), `replay(base, events)` (pull and rebuild consolidation), read/serialize.

### Branch model & the threshold map

- `master`: derived files committed (ADR-0003 exception), `events.log` always empty, carries the
  consolidation threshold map `{ "<user>": <ts_ns> }`.
- `users/<user>`: `events.log` tracked; entity folders `.gitignore`d (the `.gitignore` content is
  derived from the schema's fixed-folder set — that derivation is defined in Spec 0001 / regenerated by
  migrations; the manager only relies on it holding).
- **Threshold-map carrier — decision:** publish the map as a **tracked JSON file committed on `master`
  alongside the derived snapshot** (e.g. a well-known metadata file), rather than a git note. Rationale:
  git notes are awkward to push/fetch reliably over an arbitrary remote and easy to lose; a tracked file
  travels with the snapshot atomically, diffs cleanly, and is trivially readable during `sync`/`pull`.
  CONTEXT calls this a "note"; the *contract* is the `{ user: ts_ns }` shape associated with the
  consolidated `master` commit, and this spec fixes the carrier as a committed file. (Open only to the
  exact filename/location, to be pinned in implementation.)

### init dispatch (1.1 / 1.2 / 1.3)

- **1.1 empty remote:** create `master` = current workspace's derived files **minus** any events (empty
  `events.log`) + an empty/initial threshold map; create and check out `users/<me>`.
- **1.2 existing remote, no `users/<me>`, local pre-existing work:** create `users/<me>`; for each
  existing local entity, `createEntity` a retroactive `CREATE` (full snapshot) timestamped at the
  association moment (monotonic clock), logged into the new `events.log`; then behave like a joining peer
  (checkout `master`, replay).
- **1.3 existing remote *with* `users/<me>` + local pre-existing work:** unreconcilable. `init` **requires
  an explicit interactive confirmation and exposes no bypass flag** (the manager surfaces this as a
  required `confirmDestroy` decision provided by the caller; without it, it refuses). On confirm: local
  `tasks/`/`boards/` are permanently deleted (no backup) and replaced by a checkout of the remote
  `users/<me>` + `master` re-derive.

### sync (2.1 / 2.2) and pull

- **2.1 `master` not advanced** (its threshold map / commit unchanged since last seen): `git add` +
  `commit` + `push` `events.log` to `users/<me>`.
- **2.2 `master` advanced:** read `threshold[me]` from `master`'s map → `purge(threshold[me])` locally →
  `pull()` (checkout `master` + `replay` remaining local events over it) → commit + push the compacted
  `events.log`.
- **`pull`** is the shared "checkout `master` + `replay` local state" primitive, with **no** purge and
  **no** push; `sync` 2.2 composes it. `pull` is also a standalone user-facing plumber command.

### rebuild algorithm

1. `sync` (publish own latest events).
2. In memory only: for each `users/<user>` branch, checkout/parse (`parseLog`) and collect; concatenate
   with own; order by timestamp. Record per-peer max timestamp → threshold map.
3. `replay` the ordered events to produce the consolidated `master` snapshot (derived files).
4. Commit to `master` (empty `events.log` + threshold-map file); **push non-fast-forward**. If rejected,
   restart from step 2 against the new `master`.
5. Return to own working branch and run a normal `sync` — that sync (using the just-published threshold)
   is what purges the rebuilder's own branch. `rebuild` writes **only** `master`, never any
   `users/<user>`.

### truncate history

- Keeps last N commits of the target branch; caller may only target an owned branch (`users/<me>` or
  `master`) — targeting `users/<other>` is refused.
- Refuses to cut below the last consolidated point known from a `rebuild` (derived from `master`'s
  threshold map), so no not-yet-shared event is ever destroyed.

### disassociation

- Detaches the workspace from its remote: stops event-logging (workspace reverts to Spec 0001
  local-workspace behavior), keeps current local derived files, and **leaves all remote branches
  untouched** (requisito inicial: remote content is preserved on disassociate).

## Testing Decisions

**What a good test looks like here.** Test **external, observable git and filesystem state at the
manager public API seam** — never the private sequence of git commands issued. A test operates against
**real git** (Bun's git CLI): a temporary bare repository acting as the remote, plus one or more
temporary working directories acting as distinct peers (each with its own injected clock and git
identity, reusing the injectable seams from Spec 0001). Assertions are on: which branches exist; the
contents of `users/<user>/events.log`; `master`'s derived files and its (empty) `events.log`; the
threshold-map file; commit counts after `truncate`; and the re-derived entity files a peer ends up with.

**Primary seam (committed by this spec): the manager public API against temp git repos.** Behavioral
cases to cover:

- **init 1.1:** empty remote → `master` with derived snapshot + empty `events.log`, `users/<me>` created;
  entity folders gitignored on the working branch.
- **init 1.2:** local pre-existing entities → `users/<me>/events.log` contains exactly one retroactive
  `CREATE` per local entity, all timestamped at association; joining then re-derives `master` state.
- **init 1.3:** remote branch already present + local work → refuses without `confirmDestroy`; with it,
  local `tasks/`/`boards/` are replaced by the remote checkout (asserted by content, no backup left).
- **sync 2.1:** `master` unchanged → a new commit on `users/<me>` with the events; `master` untouched.
- **sync 2.2:** after a rebuild advanced `master` → own events with `ts <= threshold[me]` are gone from
  `events.log`, events with `ts > threshold[me]` remain, and re-derived local files match
  `master`+remaining-events.
- **purge safety:** an event authored *after* the consolidated threshold is never dropped by a following
  sync (paired with the monotonic clock).
- **pull:** checks out `master` and re-derives local state without changing `events.log` and without
  pushing.
- **rebuild convergence:** two peers each create/sync distinct edits; one rebuilds; both re-sync → both
  peers' re-derived `tasks/`/`boards/` are byte-identical and match `master`.
- **rebuild does not touch working branches:** after a rebuild, third-party `users/<other>` branches are
  unchanged (same tip commit).
- **non-ff rebuild retry:** simulate `master` advancing between a rebuilder's collection and push
  (rejected push) → the rebuild restarts and still produces a correct consolidated `master`.
- **truncate ownership & floor:** truncating `users/<other>` is refused; truncating below the last
  consolidated point is refused; truncating above it keeps exactly the last N commits.
- **disassociate:** remote branches unchanged; local files intact; subsequent operations behave as a
  local workspace (no new `events.log` writes).

**Modules tested:** `manager` only. mdwal LWW/replay/purge correctness is already covered by Spec 0001 —
these tests exercise the *git orchestration and composition*, treating mdwal primitives as trusted.

**Prior art:** Spec 0001's mdwal seam established temp-directory tests with an **injectable clock** and
**injectable git-identity/author source**; reuse both so that (a) monotonic-timestamp/purge reasoning is
deterministic and (b) distinct peers have distinct authors without touching the machine's real git
config. `bun test` per `CLAUDE.md`; one test file per manager primitive plus a multi-peer convergence
suite.

## Out of Scope

- Porcelain wrappers `taskthing sync|rebuild|truncate` and their argument/flag ergonomics — CLI spec.
- The ink TUI feedback for these async commands (spinners, the init 1.3 **confirmation UI**) — TUI spec.
  This spec fixes only the *requirement* that init 1.3 cannot proceed without an explicit, un-bypassable
  confirmation decision; the interactive prompt itself is deferred.
- Auto-rebuild-after-X-syncs policy and the sync counter — config/porcelain spec (the manager only
  exposes `rebuild` as callable).
- `config.md` schema and keys the manager reads (default branch/remote) — config spec.
- `kv_store`, entity command surface, recurrence, theming, distribution/auto-update, migrations runner.
- The exact git commit-message wording and any cosmetic git metadata.

## Further Notes

- Language: English, matching code/CLI, preserving glossary terms (`master`, `users/<user>`, peer,
  threshold map, `sync`/`pull`/`rebuild`/`truncate`).
- The manager is deliberately the **sole** git-touching module; every other layer stays git-agnostic,
  which is what keeps them unit-testable without spinning up a repo.
- Dependency direction: manager → mdwal (Spec 0001). Nothing in Spec 0001 depends on the manager.
- ADR cross-references: LWW model — ADR-0001; git-identity/author dependency — ADR-0002;
  events-only remote versioning & `master`-as-snapshot exception & derived `.gitignore` — ADR-0003;
  per-peer monotonic timestamps make timestamp-purge safe — ADR-0006.
- The one carrier decision this spec makes beyond CONTEXT (threshold map as a committed file rather than
  a git note) is called out explicitly above so a reviewer can veto it without hunting.
