# Spec 0005 — distribution, self-update, and the migration runner

> **Scope note.** This spec covers how taskthing ships and evolves: **cross-platform binary
> distribution** (GitHub releases, semver), **self-update** (`update check` / `update apply`), and the
> **migration runner** that applies version-bound structural/data migrations to every workspace. It
> builds on mdwal (replay/folder-ops/config I/O — [Spec 0001](./0001-mdwal-core.md)), the manager's
> `.gitignore`/remote model ([Spec 0002](./0002-manager-git.md)), the porcelain command surface and
> `migrations` report ([Spec 0003](./0003-porcelain-cli-kvstore.md)), and the update spinners
> ([Spec 0004](./0004-tui-theming.md)).
>
> Source of truth: [`CONTEXT.md`](../../CONTEXT.md) (`binários e distribuição`, `migrações`,
> `configuração e temas` §binário, `update` sections; `migrationSchema`) and [`docs/adr/`](../adr).
> Especially relevant: **ADR-0003** (the `migrations/` record folder is derived local state →
> `.gitignore`d; structural migrations regenerate the `.gitignore`).

## Problem Statement

taskthing is a single binary distributed to Linux, macOS, and Windows, and it must be able to evolve
its own on-disk structure over time without a server coordinating anyone. Two hard problems come with
that:

- **Getting new versions to users safely.** There's no package manager in the loop — the binary has to
  discover a newer release for its own OS, download it, and replace itself, all from GitHub.
- **Evolving existing workspaces.** A new version may need to add a `lists/` folder, rename a field,
  restrict a config value, or change a field's type across every workspace on the machine. This can't be
  an LWW event (it's a deterministic structural change every peer applies identically from their own
  binary, not a user action to reconcile), and it can be **irreversible** — so it must be applied
  exactly once per workspace, recorded, and never silently forced on the user.

Without this layer, taskthing can ship v1 but can never ship v2 without breaking v1 workspaces.

## Solution

Build the **distribution + update + migration** layer.

- **Distribution.** A GitHub Actions workflow builds one compiled binary per OS (Linux/macOS/Windows) on
  each semver-tagged release and publishes them as release assets. Each binary knows its own version.
- **Self-update.** `update check` compares this binary's version against the latest GitHub release for
  this OS, caching the answer in `config.md` for 12h. `update apply` uses `check`, and — because an
  update may trigger an irreversible migration — **asks for confirmation by default** (the user may opt
  into silent auto-update in `config.md`) before downloading, swapping the binary, and running pending
  migrations.
- **Migration runner (structural migrations only — ADR-0007).** taskthing has **two natures** of
  migration, split by **whether the change affects the replay of entity events** (not "structural vs
  data" — that line proved imprecise, because `field rename` is data-independent yet still affects replay):
  - **Structural** (new/renamed/deleted fixed **folder**, `.gitignore` regeneration, `config.md` edits) —
    does **not** affect entity-event replay, so it is a static mdwal log (`{ version, content }`) authored
    with plumber commands, embedded in the binary, bound to a version. On update, the runner applies every
    **pending** structural migration (version-ordered) to **every workspace** by replaying its log with
    **logging OFF** (never appended to `events.log`), recording a frozen copy in the workspace's
    `.gitignore`d `migrations/` folder. This is the runner's whole job.
  - **Replay transform** (`field rename` — key remap; non-coercible `field change-type` — value rewrite):
    remaps old events onto the current schema, so it **cannot** be a one-shot log — it is a **code
    transform keyed by version, applied on replay** (migrate-on-read), living in **mdwal (Spec 0001)**,
    *not* in this runner. Never applied one-shot, never recorded in `migrations/`; implicit in every replay.
    (`field create` = zod `default`; `field delete` = ignore-unknown — neither needs a migration.) See
    ADR-0007.
  The binary is the sole source of truth for what migrations exist; `migrations/` only records which
  **structural** migrations have been applied *here*.

## User Stories

Actors: **user** (updating taskthing), **migration author** (a developer shipping a migration with a
release), **CI** (the release workflow).

### Distribution & versioning

1. As CI, I want a GitHub Actions workflow that builds a single compiled binary for Linux, macOS, and
   Windows on each release, so that users can download a native binary for their OS.
2. As CI, I want each release tagged with **semver** and each binary aware of its own version, so that
   version comparison during update is well-defined.
3. As a user, I want the binaries published as GitHub **release assets**, so that `update` can discover
   and download them without any server of ours.
4. As a migration author, I want a release to be the unit that bundles both the new binary and any
   migration(s) bound to that version, so that shipping code and shipping its migration are one step.

### Bootstrap install scripts (first acquisition)

4a. As a brand-new user with no taskthing on my machine, I want a one-line **bootstrap script** — POSIX
    `install.sh` (Linux/macOS) and `install.ps1` (Windows), fetched from a stable URL (raw repo and/or a
    release asset) — that acquires the binary for me, so that I can get started without manually finding the
    right asset. This is distinct from `taskthing install` (first-run *config*, which needs the binary to
    already exist).
4b. As that user, I want the script to **detect my OS + arch**, query the **latest GitHub release**, pick
    the matching asset, and download it, so that I always get the correct native binary.
4c. As that user, I want the script to place the binary in a conventional per-user location on `PATH`
    (`~/.local/bin/taskthing` on Unix, `chmod +x`; the Windows equivalent, e.g.
    `%LOCALAPPDATA%\Programs\taskthing`, added to the user `PATH`), so that `taskthing` is runnable right
    away.
4d. As that user, I want the script to **not** auto-run the interactive `taskthing install`, but instead
    print "now run `taskthing install`", because the first-run form needs a TTY that a `curl | sh` pipe may
    not have — so bootstrap only *prepares* the binary, deterministically.

### Update check

5. As a user, I want `taskthing update check` to compare my binary's version to the latest GitHub
   release for my OS and tell me whether an update is pending, so that I know if I'm current.
6. As a user, I want the three outcomes rendered per CONTEXT copy — "you're on the latest version",
   "there's a pending update `<current> → <target>`", or a summarized error — so that the result is
   unambiguous (rendering via Spec 0004).
7. As a user, I want the result and the check time persisted to `config.md`, so that the answer is
   remembered between invocations.
8. As a user, I want `update check` to **skip the network** and reuse the saved answer if the last check
   was less than **12h** ago, so that repeated checks are cheap and offline-tolerant.
9. As a user, I want a check older than 12h (or forced) to hit GitHub, refresh the saved
   `updateAvailable`/target and the check timestamp, so that staleness self-heals.
10. As a user, I want version comparison done by **semver** (not string compare), so that `1.10.0` is
    correctly newer than `1.9.0`.
11. As a user, I want `update check` to pick the asset matching my **OS** (and architecture), so that I'm
    never offered the wrong binary.

### Update apply

12. As a user, I want `taskthing update apply` to run `update check` under the hood and, if an update is
    pending, download the new binary and replace my local one, so that updating is one command.
13. As a user, I want `update apply` to **ask for confirmation by default** before downloading/applying,
    because an update may trigger an irreversible structural migration, so that I'm never surprised by an
    unattended irreversible change.
14. As a user, I want to opt into **silent** auto-update via `config.md`, so that I can choose unattended
    updates if I accept the risk.
15. As a user, I want `update apply` to run **pending migrations** across my workspaces immediately after
    the binary is swapped, so that my data structure matches the new binary before I use it.
16. As a user, I want a failed download/swap to leave my current binary intact (no half-replaced binary),
    so that a botched update never bricks taskthing.
17. As a user, I want the whole apply flow rendered as spinner → success/failure with summarized error
    (Spec 0004 copy: "updating taskthing…" → "taskthing updated to version `<target>`"), so that I get
    clear progress and outcome.

### Binary config

18. As a user, I want `config.md` to hold my binary settings — current version, last-update-check time,
    the cached update-available/target, and the auto-update mode (confirm vs silent) — so that update
    behavior is inspectable and configurable.
19. As a user, I want the auto-update mode to **default to confirm** (not silent), so that an
    irreversible migration is never applied without my say-so.

### Migrations — model & authoring

20. As a migration author, I want a **structural** migration expressed as a **static mdwal log** authored
    with plumber commands (`entity …`, `config …`, folder ops, all with `--log`), embedded in
    the binary as `{ version, content }`, so that a data-independent structural change is a recorded,
    deterministic sequence of mdwal ops. A **data** migration (non-coercible `field change-type`) is
    instead a **code transform keyed by version** applied on replay (ADR-0007), because the dev has no
    access to the user's values and so cannot author concrete rewrite events ahead of time.
21. As a migration author, I want the **binary** to be the single source of truth for which migrations
    exist and what they do, so that no workspace needs to carry the migration definition.
22. As a migration author, I want **structural** migrations replayed with **logging OFF** and **never**
    written to any `events.log`, so that a structural change every peer applies identically is not mistaken
    for a user-authored LWW event. (Data migrations write nothing either — they transform events in memory
    on replay; the log stays immutable and unmigrated. ADR-0007.)
23. As a migration author, I want a migration ideally bound one-to-one to a specific version, so that the
    mapping from release → structural change is clear.

### Migrations — running

24. As a user, I want the runner to determine **pending** migrations by comparing the binary's embedded
    set to the workspace's `migrations/` records, so that only not-yet-applied migrations run.
25. As a user, I want pending migrations applied in **version order**, so that structural changes compose
    predictably.
26. As a user, I want each migration applied to **every workspace** on my machine (local and remote), so
    that all my workspaces stay structurally consistent with the binary.
27. As a user, I want each applied migration recorded as a frozen `<migration_id>.md` in that workspace's
    `migrations/` folder, so that it is never re-applied and I have a record of what ran.
28. As a user, I want the `migrations/` folder `.gitignore`d and never committed to `users/<user>`, so
    that each peer decides independently, from its own binary, what it has applied (ADR-0003) — no
    cross-peer sync of application state.
29. As a user, I want the runner to be **idempotent** — re-running with no new migrations changes nothing
    — so that update/apply is safe to repeat.
30. As a user, I want a migration that creates/renames/deletes a **fixed entity folder** (e.g. `lists/`)
    to regenerate the remote workspace's `.gitignore` from the new fixed-folder set (ADR-0003), so that
    the ignore rules track the schema automatically.
31. As a user, I want a migration able to alter **`config.md`** (add/remove/rename config fields, or
    restrict previously valid values — e.g. dropping american date format), so that configuration schema
    can evolve too.
32. As a migration author, I want that when an existing value becomes invalid under the new schema, the
    rewrite to be **an explicit code transform keyed by version, applied on replay** (ADR-0007) — mdwal
    never guesses a coercion — so that migration is always deterministic and author-controlled, and the
    migrated value survives every later `pull`/`sync`/`rebuild` instead of being reverted by replay. (For
    `config.md`, which lives outside the event/LWW domain, the value rewrite is applied directly by the
    structural migration, since config is not replayed from a log.)
33. As a user, I want `taskthing migrations` (Spec 0003) to report applied-vs-pending by comparing the
    binary's set to this workspace's records, so that I can audit whether I'm fully migrated.

### Cross-cutting

34. As a user, I want a migration failure on one workspace surfaced clearly (and to not silently mark that
    migration as applied there), so that a partial migration is visible and retryable.
35. As a user, I want the timestamps a migration's ops carry to come from the same monotonic clock as
    mdwal (Spec 0001), so that any timestamps written during a migration stay consistent (ADR-0006).
36. As a user, I want update/migration operations to be the only place the binary self-modifies or writes
    outside a normal command, so that ordinary usage never mutates the binary or bypasses recording.
37. As a user on an older binary, I want any `pull`/`sync`/`rebuild` that encounters events (or a `master`)
    newer than my binary to **stop and tell me to update** rather than mis-read future-schema data (the
    version barrier — ADR-0007), so that a laggard can never silently corrupt data authored by an updated
    peer; the fix is to run `update apply`.

## Implementation Decisions

### Modules built

- **Release workflow (CI)** — a GitHub Actions workflow that, on a semver tag/release, compiles one
  binary per OS (via `bun build --compile` targets for linux/mac/windows) and uploads them as release
  assets. Verified by CI producing the artifacts; not part of the unit-test seam.
- **Bootstrap install scripts** — `scripts/install.sh` (POSIX sh) and `scripts/install.ps1` (PowerShell),
  published at a stable URL (raw repo and/or release asset). Each: detects OS + arch, queries the latest
  GitHub release, selects the matching asset, downloads it, places the binary in a conventional per-user
  `PATH` location (`~/.local/bin/taskthing` + `chmod +x` on Unix; a Windows equivalent added to the user
  `PATH`), and prints "now run `taskthing install`" — it does **not** auto-run the interactive first-run
  setup (no reliable TTY under `curl | sh`). This duplicates the orchestrator's "find latest asset for
  OS/arch" logic in shell by necessity (the binary does not yet exist). Verified manually / by a smoke job,
  not part of the unit-test seams (they shell out to GitHub and the real filesystem).
- **Update orchestrator** — `check()` and `apply()`:
  - `check()`: if `config` last-check < 12h → return cached `updateAvailable`/target; else fetch the
    latest GitHub release, select the asset for this OS/arch, **semver**-compare against the running
    version, persist result + timestamp to `config.md`.
  - `apply()`: run `check()`; if pending, gate on auto-update mode (**confirm by default**, silent only if
    configured) → download the OS asset → atomically swap the local binary (download-then-replace so a
    failure leaves the old binary intact) → invoke the **migration runner** across all workspaces.
  - Rendered through Spec 0004 spinners; network access via an injectable **release source**, download via
    an injectable **downloader**, and the binary swap via an injectable **filesystem** boundary.
- **Migration runner (structural only — ADR-0007)** — given the binary's embedded **structural** migration
  set (`{ version, content }[]`) and the set of workspaces. Data migrations are *not* here — they are
  replay-time transforms in mdwal (Spec 0001) and never pass through the runner. The runner:
  - compute pending = embedded − recorded (per workspace `migrations/`), ordered by version;
  - apply each by replaying its static mdwal log through mdwal with **logging OFF** (folder ops,
    `config.md` edits — including any direct `config.md` value rewrite, since config is not replayed from a
    log; `field rename`/`change-type` are **not** here — they are replay transforms in Spec 0001);
  - regenerate the remote workspace `.gitignore` when the fixed-folder set changed (ADR-0003);
  - write a frozen `<migration_id>.md` record into the workspace's `.gitignore`d `migrations/` folder;
  - idempotent; a per-workspace failure is surfaced and does **not** record that migration as applied.
  - The embedded migration set is an injectable input (so tests supply their own); real builds embed it.
- **Binary config keys** — `config.md` gains: current version, last-update-check timestamp, cached
  `updateAvailable`/target, auto-update mode (`confirm` default | `silent`). (The `config` read/write path
  and TUI are Specs 0001/0003/0004; this spec only defines the *keys' meaning* and defaults.)

### Reuse, not redefinition

- mdwal (Spec 0001): replay/apply with logging off, folder ops, `config.md` schema I/O, monotonic clock.
- manager (Spec 0002): the remote-workspace model and the fact that `.gitignore` derives from the
  fixed-folder set — the runner regenerates it; the manager relies on it.
- porcelain (Spec 0003): plumber `entity`/`field`/`config` commands are how migrations are **authored**;
  `taskthing migrations` reports state; the migrations `kv_store` numbers them for that report.
- TUI (Spec 0004): all update spinners/messages and any confirmation prompt.

### Data-migration ⇄ remote-replay interaction (RESOLVED — ADR-0007)

Structural migrations (new/renamed folders, `.gitignore`) are unambiguous: every peer applies them
identically from its own binary, nothing is logged, and there is nothing to reconcile. **Data** migrations
(`field change-type` rewriting each entity's value) were the open design gap: rewriting only the *derived*
files with logging off does **not** survive, because the original `CREATE`/`UPDATE_FIELD` events still
carry old-typed values and the next `pull`/`sync`/`rebuild` re-derives from them — silently reverting the
migration (this bites even a single fully-updated peer on a non-coercible change; z.coerce only saves
coercible ones).

**Resolution (ADR-0007): migrate-on-read with versioned events.** Every event carries the semver of the
binary that authored it; the binary embeds the `version → schema` map. A data migration is a **code
transform keyed by version**, applied by mdwal `replay` (Spec 0001): before folding an event whose version
is older than the current schema, replay runs the transform chain so the value is materialized under the
current schema. The log stays immutable and unmigrated; derived state is always current-schema; the
migrated value survives every replay because it is *produced by* replay. The rewritten value inherits the
original event's timestamp, so purge/threshold (ADR-0006) is unaffected. **Version barrier:** an event
newer than the running binary aborts replay with an "update taskthing" error (never a best-effort read of
future-schema data); `master` is stamped with its build version and an older binary refuses `pull`;
`rebuild` refuses if any collected peer event is newer than the rebuilder. This resolves the former
story 22/32 contradiction — there is no un-logged rewrite event to lose, because the data rewrite is a
replay-time transform, not a one-shot derived write. The same log-vs-derived observation for `clear`
(Spec 0003) is *intended* behavior there and needs no change.

## Testing Decisions

**What a good test looks like here.** Test **external behavior at two API seams with the environment
injected** — never by hitting real GitHub or swapping a real binary. Update tests inject a **release
source**, a **downloader**, a **clock**, and a **config**; migration tests inject the **embedded migration
set** and run against a **temp workspace tree**. Assert on: the update decision (pending/latest/error),
the 12h cache behavior, what got downloaded/swapped (via the injected fs), and — for migrations — the
resulting workspace structure, `config.md`, `.gitignore`, and `migrations/` records.

**Seam 1 (committed): the update orchestrator** (injected release source / downloader / clock / config):

- **semver comparison:** `1.10.0` > `1.9.0`; equal → "latest"; older running version → pending
  `<current> → <target>`.
- **OS/arch asset selection:** picks the asset for the injected platform; missing asset → clean error.
- **12h cache:** a check within 12h returns the cached answer without touching the release source; a check
  past 12h (or forced) refreshes and re-persists result + timestamp.
- **apply gating:** default mode requires a confirmation before download/swap; silent mode (configured)
  proceeds without; a rejected confirmation applies nothing.
- **atomic swap:** a download failure leaves the old binary in place (no half-written binary); a success
  swaps and then invokes the migration runner.

**Seam 2 (committed): the migration runner** (injected embedded set; temp workspaces):

- **pending detection:** only migrations absent from a workspace's `migrations/` run; ordered by version.
- **replay with logging off:** applying a migration mutates the workspace (e.g. creates `lists/`) but
  appends **nothing** to `events.log`.
- **recording & idempotency:** each applied migration writes a frozen `<migration_id>.md`; a second run
  applies nothing.
- **`.gitignore` regeneration:** a migration adding/removing a fixed folder regenerates a remote
  workspace's `.gitignore` from the new fixed-folder set; `migrations/` stays ignored.
- **config migration:** a migration that restricts a config value includes the explicit direct rewrite of
  `config.md` (config is not replayed from a log) and leaves it valid under the new schema; mdwal never
  coerces on its own. (Entity *data* rewrites are replay transforms tested in Spec 0001, not here.)
- **all workspaces:** a pending migration is applied to every workspace in the injected tree (local +
  remote).
- **failure handling:** a migration that fails on a workspace is surfaced and is **not** recorded as
  applied there (so it retries next run).

**Modules tested:** the update orchestrator and the migration runner — through their seams with injected
environment. mdwal replay/LWW (0001), git (0002), command logic (0003), and rendering (0004) are trusted,
not re-tested. The **CI release workflow** is validated by CI producing per-OS artifacts, outside the
unit-test seams.

**Prior art:** reuse the injectable **clock** (0001) and **config** (0003/0004); add injectable **release
source**, **downloader**, and **filesystem/binary-swap** boundaries so no test touches the network or the
real binary. `bun test` per `CLAUDE.md`; one suite for the update orchestrator, one for the migration
runner.

## Out of Scope

- The interactive rendering of update spinners and the confirmation prompt — Spec 0004 (this spec drives
  them; it doesn't draw them).
- `config.md` read/write mechanics and the config TUI — Specs 0001/0003/0004 (this spec only defines the
  binary keys' meaning/defaults).
- mdwal replay/LWW internals (0001), git orchestration (0002), and command parsing/kv_store (0003).
- Authoring UX for migrations beyond "use the existing plumber commands with `--log`" — no dedicated
  migration-authoring tool is specified.
- Rollback/downgrade of migrations (CONTEXT treats migrations as forward-only, potentially irreversible;
  no undo is specified).
- Package-manager/distribution channels other than GitHub releases (Homebrew, apt, etc.) — not in CONTEXT.

## Further Notes

- Language: English, matching code/CLI, preserving glossary terms (migration runner, `migrations/`
  record, semver, embedded migration set).
- Tooling fixed by CONTEXT: binaries via Bun's compile targets; releases and update discovery via GitHub.
- **Forward-only, per-workspace, per-peer:** *structural* migrations are never rolled back, are recorded
  per workspace, and each peer applies them independently from its own binary — there is no shared
  migration state. *Data* migrations are not recorded at all (implicit in replay), and are also
  forward-only (the version barrier prevents an older binary from reading newer-schema events).
- **Data-migration vs. replay — RESOLVED (ADR-0007):** a data migration is a version-keyed **code
  transform applied on replay** (mdwal, Spec 0001), so its result is *produced by* every `pull`/`sync`/
  `rebuild` rather than reverted by them; the log stays immutable and versioned, and a version barrier
  stops an older binary from mis-reading newer events. The old `clear` log-vs-derived tension is intended
  behavior and unchanged.
- Dependency direction: this layer → mdwal (0001) for replay, → manager's `.gitignore`/remote model
  (0002), → porcelain (0003) for authoring/report, → TUI (0004) for feedback. It is the top of the stack;
  nothing depends on it.
- ADR cross-references: `migrations/` as gitignored derived state and `.gitignore` regeneration on
  fixed-folder change — ADR-0003; monotonic timestamps for any migration-written ts — ADR-0006;
  migrate-on-read, versioned events, the structural-vs-data migration split & the version barrier — ADR-0007.
