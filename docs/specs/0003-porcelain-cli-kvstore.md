# Spec 0003 — porcelain CLI, kv_store, and task-domain command logic

> **Scope note.** This spec covers the **porcelain command layer** — the daily-driver CLI a user
> actually types — plus the **`kv_store`** (ephemeral number↔nanoid mapping) and the **task-domain
> logic** those commands encode (the `add` natural-language parser, task-date/recurrence semantics,
> `list` filtering). It builds on the `Repository<Model>` and mdwal contracts from
> [Spec 0001](./0001-mdwal-core.md) and the manager primitives from [Spec 0002](./0002-manager-git.md).
>
> **Rendering is out of scope.** Every interactive/visual concern — the ink TUI, list styling,
> spinners, selection/confirmation/textarea UIs, the `config` TUI, `theme`, ANSI theming — belongs to
> the TUI spec (0004). This spec fixes command *behavior* and the *data* commands produce; 0004 fixes
> how that data is drawn. **Distribution is also out of scope** — `taskthing update check|apply` and
> the binary side of `install` belong to the distribution spec (0005).
>
> Source of truth: [`CONTEXT.md`](../../CONTEXT.md) (`kv store`, `comandos`, `models` sections) and
> [`docs/adr/`](../adr). Especially relevant: **ADR-0003** (the kv_store `.json` files are derived
> local state → `.gitignore`d) and **ADR-0004** (the `"inbox"` board sentinel drives `--in-board=inbox`
> and the task `board` default).

## Problem Statement

The lower layers speak in 21-char nanoids and raw mdwal operations. A person managing tasks from a
terminal can't be expected to type `taskthing star V1StGXR8_Z5jdHi6B-myT` — they want `taskthing star 3`.
They also want to jot a task in one line — `taskthing add "walk the dog r:[every monday] d:[tomorrow]"` —
and have the date and recurrence understood, not hand-encoded. And they want the ergonomic verbs of a
real task manager (`add`, `check`, `star`, `list`, `boards`) sitting over the plumber primitives,
without a namespace prefix, following git's plumbing/porcelain split.

Three gaps stand between the primitives and that experience:

- **No ergonomic identity.** Users need stable-enough short numbers for entities, mapped to nanoids,
  refreshed on each listing — and taskthing is a short-lived CLI with no in-memory state between
  invocations, so that mapping must live on disk.
- **No command surface.** There is no dispatch, no argument conventions, no natural-language `add`
  parser, no `list` filtering, no wiring from verbs to repositories/manager.
- **No task-domain logic.** The rules for how a task's date lives inside `rrule`, how completing a
  recurring task mints the next occurrence, and how `list` filters compose, exist only as prose in
  CONTEXT and must become executable command behavior.

## Solution

Build the **porcelain layer**: a single-binary CLI with git-style plumbing/porcelain coexistence (no
prefix), the **`kv_store`**, and the task-domain command logic.

- **CLI dispatch & conventions.** One binary. The two layers signal themselves through syntax: plumber
  takes the entity name as a **positional** argument (`taskthing entity create task …`); porcelain takes
  it as a **flag** (`taskthing add --board …`). The default entity is `task`; `--workspace` is optional
  and falls back to the current workspace in `config.md`. **Note (ADR-0007):** the `field
  create/rename/delete/change-type` plumber commands are **dev-time migration-authoring tools, not a
  runtime user capability** — schema lives in the binary and is never mutated at runtime; they are out of
  scope as user-facing commands here.
- **`kv_store`.** Three independent on-disk stores (tasks, boards, migrations), each a `.gitignore`d
  `.json` in the workspace, mapping ephemeral numbers `1,2,3,…` ↔ nanoid. Rebuilt on every `list`. Read
  fresh from disk on every invocation (no RAM cache — each command is a new process).
- **Task-domain logic.** The `add` natural-language parser (chrono for dates, rrule for recurrence, with
  a strict `tag:[…]` bracket grammar), the single-source-of-truth date-in-`rrule` semantics, the
  recurrence-on-`check` occurrence minting, and `list` filter composition.
- **Command wiring.** Every porcelain verb resolves numbers → nanoids via `kv_store`, then calls the
  Spec 0001 repositories or Spec 0002 manager primitives. Where a command needs interactive input or
  styled output, it produces/consumes plain data and delegates drawing to 0004.

## User Stories

Actors: **user** (daily terminal use), **scripter** (non-interactive/scriptable use), **new user**
(first run / install).

### CLI conventions

1. As a user, I want plumber and porcelain commands to live in one binary with no `plumber`/`porcelain`
   prefix (git-style), so that the CLI stays terse.
2. As a user, I want the layer I'm in signalled by syntax — plumber uses a positional entity name,
   porcelain uses a `--<entity>` flag — so that I can tell at a glance which level I'm operating at.
3. As a user, I want `task` to be the default entity for porcelain verbs, so that the common case
   (`taskthing add "…"`) needs no `--task`.
4. As a user, I want `--workspace=<name>` to be optional on every workspace-scoped command, defaulting to
   the current workspace from `config.md`, so that I rarely pass it.
5. As a scripter, I want unknown commands/flags and malformed input to fail with a clear non-zero exit
   and message, so that scripts can detect errors.

### kv_store (ephemeral number↔nanoid)

6. As a user, I want listed tasks/boards shown as small numbers (`1, 2, 3, …`) instead of nanoids, so
   that I can act on them by number.
7. As a user, I want those numbers refreshed every time I run a `list` in a workspace, so that the
   numbering always reflects the latest listing.
8. As a user, I want the numbers to be a short-lived convenience, not stable ids — the most recent
   `list` wins, with no cross-terminal concurrency protection — so that the model stays simple (the
   same trade-off already accepted for ephemeral numbering).
9. As a user, I want three independent stores — tasks, boards, migrations — each in its own workspace
   `.json`, so that numbering in one entity type never collides with another.
10. As a user, I want those `.json` files `.gitignore`d, so that derived local numbering is never shared
    or committed (ADR-0003).
11. As the CLI, I want the store read fresh from disk on every invocation (no RAM cache), so that
    resolving `task 3` in a new process always sees the latest mapping.
12. As the CLI, I want `get` to be **bidirectional** (number→nanoid and nanoid→number), so that `list`
    can populate the store while iterating entities and command resolution can go the other way.
13. As the CLI, I want the store to expose `save`, `load`, `set`, `get`, `reset`, so that `list` can
    reset-then-repopulate and verbs can resolve.
14. As the CLI, I want `reset` to operate directly on disk, accepting that a crash mid-repopulation can
    leave the `.json` empty/inconsistent until the next successful `list`, so that the store stays a
    zero-ceremony short-term reference (acceptable per CONTEXT).
15. As a user, I want a command referencing a number that isn't in the store (never listed, or store
    reset) to fail with a message telling me to run `list` first, so that stale references fail loudly
    rather than acting on the wrong entity.

### First run (install — non-UI behavior)

16. As a new user, I want `taskthing install` to scaffold `~/.config/taskthing/` with a local workspace
    (`tasks/`, `boards/`) and a `config.md`, so that I can start immediately with no remote.
17. As a new user, I want `install` to persist my chosen date format (american `yyyy/mm/dd` or european
    `dd/mm/yyyy`) and my nerdfont support (default: no) into `config.md`, so that later rendering and
    parsing respect them.
18. As a new user, I want re-running `install` to be safe (not clobber an existing config/workspace
    without cause), so that I can't accidentally wipe my setup.

### Workspaces

19. As a user, I want `workspace list|create|rename|delete` and `workspace use`, so that I can manage
    multiple task folders.
20. As a user, I want `workspace delete <name>` to require confirmation (destructive, removes the local
    folder), so that I can't wipe a workspace by accident.
21. As a user, I want deleting the *currently active* workspace to be blocked with an error until I
    switch away, so that I'm never left without a current workspace.
22. As a user, I want `taskthing use <name>` as a first-class shortcut for `workspace use`, so that
    switching workspaces is one short command.
23. As a user, I want the current workspace changeable **only** via `use`/`workspace use` (never through
    the generic `config` surface), so that there's one obvious way to switch.
24. As a user, I want `workspace remote add <url>` to associate the workspace with a remote (invoking the
    manager `init` flow, Spec 0002) and `workspace remote remove` to disassociate (preserving remote
    content), so that a workspace can go remote or local at any time.

### Config (scriptable)

25. As a scripter, I want `config get|set|delete <key> [value]` to read/write config non-interactively
    with friendly validation, so that configuration is scriptable without opening a TUI.
26. As a scripter, I want `config set` to reject invalid values (e.g. a date format outside the allowed
    set) with a clear error, so that I can't persist an invalid config.
27. As a user, I want the plumber `config get|set|delete` and the porcelain equivalents to share syntax,
    so that muscle memory carries over (the porcelain one adds validation/confirmation).

### Tasks — capture (`add`)

28. As a user, I want `taskthing add "<input>"` to create a task from one string, so that capture is
    frictionless.
29. As a user, I want the parser to extract a date from `d:[…]` or `date:[…]` and a recurrence from
    `r:[…]` or `recurrence:[…]`, in English, using chrono for the date and rrule for the recurrence, so
    that natural phrases like `d:[tomorrow]` and `r:[every monday]` just work.
30. As a user, I want the brackets to be **mandatory** — a `tag:` not immediately followed by `[…]`
    isn't a flag — so that free text like `"remind Ed: buy milk"` stays entirely in the title.
31. As a user, I want a matched `tag:[…]` pair removed **wholesale** from the title text, so that the
    stored title is clean (`"walk the dog"`).
32. As a user, I want `add "walk the dog"` → a task with only a title (`rrule: null`), so that undated
    capture is the default.
33. As a user, I want `add "walk the dog d:[tomorrow]"` → a dated but non-recurring task (`rrule` with
    `DTSTART` only, no `RRULE`), so that a one-off date needs no recurrence.
34. As a user, I want `add "walk the dog r:[every monday]"` → a recurring task (`rrule` with `RRULE`), so
    that recurrence alone is expressible.
35. As a user, I want `add "walk the dog d:[tomorrow] r:[every monday]"` → recurrence whose `DTSTART` is
    exactly the provided date, so that I control where a recurrence starts.
36. As a user, I want a new task's `board` to default to the sentinel `"inbox"` when I don't assign one
    (ADR-0004), so that everything I capture lands somewhere.

### Tasks — edit & state

37. As a user, I want `star <n>` / `check <n>` / `uncheck <n>` / `delete <n>`, so that I can change task
    state by number.
38. As a user, I want `delete` to soft-delete (via `Repository.delete`) and be recoverable, so that I
    never lose a task irrecoverably from a normal delete.
38a. As a user, I want `recover <n>` to un-delete a soft-deleted task (via `Repository.recover` →
    `updateField(deleted, false)`), resolving `<n>` from a `list --deleted` view, so that I can bring back
    a task I deleted by mistake. Unlike `clear` (a purely local, un-logged file removal), `recover` is a
    **real logged LWW event** — the inverse of `delete` — so it propagates to peers and converges. Boards
    mirror this with `recover --board <id>` (inverse of `delete --board`).
39. As a user, I want `set <field> <n>` across `title`, `description`, and `date-time`, so that one
    `set` verb edits any of a task's fields (the field is the subcommand; the new value comes via
    interactive input — the input UI is 0004; the write is a `Repository.update`/`updateField`).
40. As a user, I want `set date-time <n>` to re-parse the date/recurrence (chrono + rrule) and, before
    saving, show me the interpreted result (e.g. `"every monday, starting tomorrow (23 jul) — confirm?"`)
    so that I catch a parsing mistake before it's stored (the confirmation UI is 0004; the *parsed
    result* it displays is produced here).
41. As a user, I want `set date-time` to rewrite the `rrule` `DTSTART`, so that a task's date has one
    source of truth and no parallel `date` field (ADR-referenced CONTEXT rule).
42. As a user, I want `clear <n>` to permanently remove an already **soft-deleted** task from my
    **local** workspace (deleting its local file) **without** emitting any shared event or touching
    other peers, so that I can tidy up local deleted-task clutter. It applies only to tasks already
    marked `deleted: true` — clearing a non-deleted task is an error. (On a remote workspace, because
    state is replay-derived, the task's `CREATE`/`deleted` events still exist, so a later
    `pull`/`sync`/`rebuild` may reconstruct the local file — `clear` is short-term tidy-up, not history
    removal.)

### Tasks — date & recurrence semantics

43. As a user, I want an open task's `DTSTART` **pinned** — never auto-advancing with the passage of
    time — so that an overdue recurring occurrence keeps its original date and renders as late
    ("N days ago") instead of silently skipping forward.
44. As a user, I want a task with no date to be `rrule: null`, and a dated-non-recurring task to be an
    `rrule` with `DTSTART` only, so that "when" is always read from `rrule`.
45. As a user, I want a completed task's displayed date to be its completion timestamp (the `completed`
    field's `lastModified`), not its `rrule`, so that completed items show when they were actually done.
46. As a user, when I `check` a task that has an `rrule`, I want a **new** entity minted (new id, new
    `CREATE`, same `rrule`) for the next occurrence while the original stays `completed`, so that each
    occurrence's history is preserved and ids are never recycled.
47. As a user, I want the new occurrence's `DTSTART` to be the **next occurrence after the completed
    entity's `DTSTART`** — advancing exactly one, even if that next occurrence is still in the past — so
    that repeatedly checking an overdue recurring task lets me catch up one occurrence at a time.
48. As a user, I want checking a recurring task whose `RRULE` is exhausted (no next occurrence) to mint
    nothing, so that finished recurrences simply stop.

### Tasks — list

49. As a user, I want `taskthing list` to show my tasks numbered (populating the tasks `kv_store`), so
    that I can then act on them by number.
50. As a user, I want `list` filters `--checked` (default false), `--starred`, `--deleted`,
    `--hasDescription`, `--period=<Nd|Nm|Ny>`, and `--in-board=<name>,…`, so that I can scope listings.
51. As a user, I want `--in-board` named distinctly from the `--board` entity-selector, so that "filter
    by board value" and "operate on boards" never collide under `list`.
52. As a user, I want `--in-board=inbox` to filter by comparing the task `board` field directly to the
    sentinel `"inbox"` (no kv_store/nanoid lookup, since inbox isn't a real board), so that inbox
    filtering works for the virtual board (ADR-0004).

### Boards

53. As a user, I want `add --board <name>` to create a board, so that I can organize tasks.
54. As a user, I want `set --board name|icon|color <id> [value]`, so that I can edit a board's
    presentation.
55. As a user, I want a board's color chosen from the 16 ANSI indices (by name, via a selection UI),
    never hex/RGB, so that board colors stay consistent with the ANSI-only theme (ADR-0005; the
    selection UI itself is 0004).
56. As a user, I want `delete --board <id>` (soft-delete) and `list --board`, so that I can manage and
    view boards.
57. As a user, I want `taskthing boards` as a first-class shortcut for `list --board`, so that viewing
    boards is one word.

### Migrations (view)

58. As a user, I want `taskthing migrations` to show which **structural** migrations are applied to the
    current workspace and whether any are pending, numbered via the migrations `kv_store`, so that I can
    audit migration state (the migration *runner* is 0005; this command only reports). Data migrations are
    version-keyed replay transforms (ADR-0007), not recorded per workspace, so they are not listed as
    applied/pending rows; the report may show the current schema version as an informational line.

### Manager verbs (porcelain wrappers)

59. As a user, I want `taskthing sync`, `taskthing rebuild`, and `taskthing truncate [--branch <b>]` as
    porcelain wrappers over the Spec 0002 manager primitives, so that I can keep a workspace shared with
    short commands (their async progress rendering is 0004).

## Implementation Decisions

### Modules built

- **CLI dispatcher** — one binary; parses argv, routes to plumber or porcelain handlers. Conventions:
  plumber = positional entity name; porcelain = `--<entity>` flag; default entity `task`; `--workspace`
  optional → falls back to `config.md` current workspace. Arg parsing via a lightweight parser
  (implementation choice; not prescribed).
- **`kv_store`** — three stores (tasks/boards/migrations), each a `.gitignore`d workspace `.json`.
  Methods `save`, `load`, `set`, `get` (bidirectional), `reset`. No RAM cache — disk read per
  invocation; `reset` mutates disk directly. Numbers regenerated on each `list`; last `list` wins, no
  concurrency protection.
- **`add` parser** — grammar: `(?:d|date|r|recurrence):\[ … \]`; brackets mandatory; a `tag:` without a
  closing `[…]` is not a flag (stays in title). Matched `tag:[…]` pairs are stripped wholesale from the
  title. The `d:`/`date:` payload is parsed by **chrono** (English); the `r:`/`recurrence:` payload by
  **rrule**. Composition into the `rrule` field: none → `null`; date only → `DTSTART` only; recurrence
  only → `RRULE`; both → `RRULE` with `DTSTART` = the provided date.
- **Task-domain logic** — date lives only in `rrule` (no parallel field); open-task `DTSTART` is pinned;
  completed-task displayed date = `completed` field `lastModified`; `set date-time` rewrites `DTSTART`.
  `check` on an `rrule` task mints a new entity (new nanoid, new `CREATE`, same `rrule`), `DTSTART` = the
  next `RRULE` occurrence strictly after the completed entity's `DTSTART` (advance exactly one, even if
  still past); exhausted `RRULE` → mint nothing; original stays `completed`.
- **`clear`** — a purely **local** cleanup, distinct from `delete` (soft-delete): it physically removes
  the local file of a task that is **already** `deleted: true`, emits **no** event, does not go through
  LWW, and does not propagate to peers. Errors if the target isn't already deleted. On a remote
  workspace the `CREATE`/`deleted` events persist, so a later `pull`/`sync`/`rebuild` may reconstruct the
  file — `clear` is short-term local tidy-up, not history removal.
- **`list` query** — resets then repopulates the relevant `kv_store` while iterating; applies filters
  `--checked` (default false), `--starred`, `--deleted`, `--hasDescription`, `--period`, `--in-board`
  (with `--in-board=inbox` comparing to the `"inbox"` sentinel directly). Produces an ordered list of
  entities as **plain data**; styling is 0004.
- **Command→lower-layer wiring** — verbs resolve number→nanoid via `kv_store.get`, then call Spec 0001
  repositories (`create/update/delete/recover/findById/findAll/filter`) or Spec 0002 manager primitives
  (`sync/rebuild/truncateHistory`, and `init`/`disassociate` via `workspace remote add`/`remove`).

### Command surface (behavior only; rendering deferred to 0004)

- **setup:** `install` scaffolds `~/.config/taskthing/` + local workspace + `config.md`; persists date
  format + nerdfont. (The interactive form UI and the binary-download side are 0004/0005.)
- **workspace:** `list|use|create|rename|delete`, `remote add|remove`, and the `use` shortcut. `delete`
  is confirmed and blocked for the active workspace.
- **config:** `get|set|delete <key> [value]` scriptable with validation. Current-workspace key excluded
  (only `use` changes it). The interactive `config`/`theme` TUIs are 0004.
- **tasks:** `add`, `star`, `check`, `uncheck`, `delete`, `recover`, `clear`, `set title|description|date-time`,
  `list` (+ filters).
- **boards:** `add --board`, `set --board name|icon|color`, `delete --board`, `recover --board`,
  `list --board` (+ `boards` alias). Board color chosen from ANSI-16 names.
- **migrations:** `migrations` (report only).
- **manager:** `sync`, `rebuild`, `truncate [--branch]` (wrappers over Spec 0002).

### Explicitly deferred

- All ink rendering: list styling, spinners, selection/confirmation/textarea UIs, `config` TUI, `theme`,
  ANSI theming — Spec 0004.
- Distribution: `update check|apply`, binary download, `install`'s binary/self-update side, the migration
  *runner* — Spec 0005. (`taskthing migrations` here only *reports*.)
- Auto-rebuild-after-X-syncs policy — config/manager concern, not a command behavior here.

## Testing Decisions

**What a good test looks like here.** Test **external command behavior at the CLI-entrypoint seam** —
invoke `taskthing <cmd> …` in a temporary `$HOME` + workspace and assert on (a) the resulting workspace
files (task/board `.md` via mdwal `read`, `config.md`), (b) the `kv_store` `.json` contents, and (c) the
command's **plain data output / exit code**. Do **not** assert ANSI styling or ink layout — that's 0004,
tested below this seam.

**Primary seam (committed): the CLI entrypoint.** Behavioral cases:

- **`add` parser:** title-only → `rrule: null`; `d:[tomorrow]` → `DTSTART` only; `r:[every monday]` →
  `RRULE`; both → `RRULE` + provided `DTSTART`; `"remind Ed: buy milk"` → whole string stays the title
  (no bracket, no flag); matched `tag:[…]` stripped wholesale from the stored title; new task defaults
  `board = "inbox"`.
- **Number resolution:** `list` populates the tasks `kv_store`; a subsequent `star 3`/`check 3` resolves
  `3`→nanoid and mutates the right entity; a number absent from the store errors telling the user to
  `list` first.
- **kv_store:** three separate stores, each a gitignored `.json`; `list` resets then repopulates; `get`
  works both directions.
- **list filters:** `--checked` default excludes completed; `--starred`, `--deleted`, `--hasDescription`,
  `--period`, `--in-board=<name>` scope correctly; `--in-board=inbox` matches tasks whose `board` is the
  sentinel without any board lookup.
- **recurrence on check:** checking an `rrule` task mints a new entity (new id, same `rrule`, `DTSTART` =
  next occurrence after the old `DTSTART`, advancing one even if still past); original becomes
  `completed`; exhausted `RRULE` mints nothing; a non-recurring dated task mints nothing.
- **date semantics:** open task `DTSTART` doesn't drift; `set date-time` rewrites `DTSTART`; a completed
  task's reported date is the `completed` `lastModified`.
- **soft-delete/recover:** `delete <n>` sets `deleted = true` (logged LWW event) and the task stays
  retrievable; `recover <n>` sets `deleted = false` (also a logged LWW event, the inverse of `delete`),
  with `<n>` resolved from a `list --deleted` view. `recover` propagates to peers; contrast `clear`, which
  is a local, un-logged file removal. Boards mirror: `delete --board <id>` / `recover --board <id>`.
- **clear:** `clear <n>` on an already-`deleted` task removes its local file and emits no event; `clear`
  on a non-deleted task errors.
- **workspace:** `delete` blocked on the active workspace; confirmed delete of a non-active one removes
  the folder; `remote add`/`remove` invoke the manager `init`/`disassociate` paths.
- **config:** `config set` rejects an out-of-set date format; valid set persists to `config.md`; the
  current-workspace key isn't settable via `config`.

**Modules tested:** the porcelain layer, `kv_store`, and the task-domain logic — through the CLI seam.
Repositories/mdwal (Spec 0001) and the manager (Spec 0002) are trusted; these tests exercise
*parsing, resolution, filtering, and wiring*, not LWW or git orchestration.

**Prior art:** Specs 0001/0002 established temp-directory tests with an **injectable clock** and
**injectable git-identity/author**. Reuse both, and additionally **inject "now" for chrono** so that
`d:[tomorrow]`/`period` parsing is deterministic in tests. `bun test` per `CLAUDE.md`; one test file per
command group plus a parser suite for `add`.

## Out of Scope

- All ink TUI rendering and ANSI theming (list styling, spinners, selection/confirmation/textarea UIs,
  the interactive `config` TUI, `theme`, board-color selection UI) — Spec 0004. This spec produces the
  data those UIs draw and consumes the values they return.
- Distribution: `update check|apply`, binary download/self-update, `install`'s binary side, the migration
  runner — Spec 0005. `taskthing migrations` here only reports state.
- LWW/replay/purge internals (Spec 0001) and git orchestration (Spec 0002) — reused, not redefined.
- Auto-rebuild policy, commit-message wording.

## Further Notes

- Language: English, matching code/CLI, preserving glossary terms (plumber/porcelain, `kv_store`,
  sentinel `"inbox"`, `rrule`/`DTSTART`/`RRULE`).
- Libraries fixed by CONTEXT: **chrono** (natural-language dates) and **rrule** (recurrence) for the
  `add`/`set date-time` parsing.
- **`clear` semantics (clarified 2026-07-22, now in CONTEXT):** `clear <n>` is a purely local removal of
  an **already soft-deleted** task's local file — no event, no LWW, no propagation; errors on a
  non-deleted task. It is *not* a field-clearing command.
- **task `set` (clarified):** `set <field> <n>` uses the field as the subcommand (`title`, `description`,
  `date-time`) and takes the new value via interactive input (0004). No separate ambiguity to resolve.
- Dependency direction: porcelain → repositories/mdwal (0001) and manager (0002). Nothing below depends
  on the porcelain layer.
- ADR cross-references: `"inbox"` sentinel driving `--in-board=inbox` and the `board` default — ADR-0004;
  kv_store `.json` as gitignored derived state — ADR-0003; board color / theme staying ANSI-only —
  ADR-0005 (rendering in 0004).
