# Spec 0001 — mdwal core (engine, schema, LWW, log, replay, repository)

> **Scope note.** This is the *foundational* spec for taskthing. It covers only the
> **mdwal** engine and the `Repository<Model>` interface that sits directly on top of it.
> The `manager` (git init/sync/rebuild/pull/truncate), the porcelain CLI, the ink TUI,
> configuration/`config.md`, the `kv_store`, theming, and distribution/auto-update are
> **out of scope** and will each get their own spec. Everything in those later layers
> depends on the contracts frozen here.
>
> Source of truth for the domain: [`CONTEXT.md`](../../CONTEXT.md) and
> [`docs/adr/`](../adr) (ADR-0001 through ADR-0006). This spec must not contradict them;
> where it restates a decision it does so to make the mdwal contract implementable.

## Problem Statement

Multiple people want to share one folder of markdown task files (a **workspace**) through a
plain git remote, editing the same tasks and boards from different machines, **without any
central server or coordination**. If those markdown files were versioned directly, two peers
editing the same task concurrently would produce a git merge conflict over YAML frontmatter —
git has no notion of "which field changed most recently", so edits would be lost or corrupted.

taskthing needs a low-level engine that:

- reads and writes markdown-with-frontmatter files against a **schema** (so values have types),
- records every change a peer makes as an immutable, append-only **event**,
- resolves concurrent edits **deterministically and per-field** (Last-Write-Wins) so that any
  peer replaying the same set of events reaches the same state,
- does all of the above the same way whether a workspace is **local** (no remote, no coordination)
  or **remote** (git-backed, shared) — the only difference being whether events are logged.

Without this engine, none of the higher layers (manager, CLI, recurrence, theming) can exist,
and shared workspaces are impossible.

## Solution

Build **mdwal** — a generic, schema-coupled markdown-write-ahead-log engine. mdwal is the single
low-level module that performs *every* structural and data operation on a workspace: create /
rename / truncate / delete entity folders; create / update / soft-delete frontmatter fields on
markdown files; parse and serialize the `events.log`; and resolve LWW.

mdwal is bound to a **zod schema** that declares the valid fields of each entity type (task, board,
…). The schema is the *only* source of truth for field types — the YAML frontmatter carries bare
values, never type information. From the schema, mdwal derives, for every field, the companion LWW
metadata (`<field>_lastModified` in nanoseconds and `<field>_lastModifiedBy` = author) and the
derived read-only fields `createdAt` / `updatedAt`.

Each operation is **atomic** — exactly one field-change, one create, or one folder op per event,
never a combination. In a **remote** workspace each operation may append exactly one line to
`events.log`; in a **local** workspace the same operation is applied directly to the file with no
log and no LWW (there is no concurrency to resolve). Events are timestamped with **per-peer
monotonic nanosecond timestamps** so the log is strictly increasing, which is what makes
timestamp-based purge safe.

On top of mdwal, a thin **`Repository<Model>`** adapter exposes CRUD-shaped access (`create`,
`update`, `delete`, `recover`, `findById`, `findAll`, `filter`) per entity type, translating model
objects to and from mdwal operations and reads.

The plumber `entity` / `field` commands (defined in a later CLI spec) are the user-facing surface
of these same mdwal operations; this spec defines the engine they call, not the commands.

## User Stories

Actors: **peer** (a person sharing a workspace from one machine), **consuming layer** (the
manager / repositories / CLI built on mdwal), **migration author** (a taskthing developer writing
a versioned migration log).

1. As a peer, I want every change I make to a task to be recorded as an immutable event, so that my
   history is never silently rewritten.
2. As a peer, I want each event to describe exactly one atomic action (one field change, one create,
   one folder op), so that partial or ambiguous changes can never be recorded.
3. As a peer, I want creating a task to be recorded as a single `create` event carrying the entity's
   full initial snapshot, so that a new entity is never reconstructed from N separate field events.
4. As a peer, I want updating a single field to be recorded as one `update-field` event, so that the
   field's value and its LWW metadata move together as one atomic write.
5. As a peer, I want deleting a task to be a soft-delete (a field update `deleted = true`), so that
   the file is never physically removed and the deletion itself is subject to LWW.
6. As a peer, I want to recover a soft-deleted task (set `deleted = false`), so that an accidental
   deletion can be undone through the same field-update mechanism.
7. As a peer editing the same field as someone else concurrently, I want the edit with the most
   recent nanosecond timestamp to win, so that our edits converge to the same value on every machine.
8. As a peer, I want LWW resolved **per field**, so that my edit to a task's `title` is not lost just
   because someone edited its `description` afterward.
9. As a peer whose edit loses an LWW race, I want my event still written verbatim to my log (just not
   applied to the state), so that the log remains a faithful record of everything I authored.
10. As a peer, I want the applied state to be derivable purely by replaying events under LWW, so that
    any two peers with the same events reach byte-identical derived files.
11. As a peer, I want each event's timestamp generated as `max(now_utc_ns, last_local_ts_ns + 1)`,
    so that my `events.log` is strictly increasing even if my wall clock steps backward.
12. As a peer, I want two successive edits I make to the same field to never be self-inconsistent
    under LWW, so that my own later edit always wins over my own earlier edit.
13. As a peer, I want the author of every change taken from my machine's global git `user.name`, so
    that changes are attributed correctly even in a purely local workspace with no git repo.
14. As a peer, I want `createdAt` and `updatedAt` derived automatically (never stored as schema
    fields), so that they can't drift from the actual event history.
15. As a peer, I want a task's `updatedAt` to equal the greatest `<field>_lastModified` across all its
    fields, so that "last touched" reflects the real most-recent field change.
16. As a peer, I want a task's `createdAt` to equal the timestamp of its `create` event, so that
    creation time is stable regardless of later edits.
17. As a consuming layer, I want mdwal reads to parse a markdown file's frontmatter into a typed JSON
    object via the entity's zod schema, so that I get validated, coerced values rather than raw YAML.
18. As a consuming layer, I want mdwal writes to serialize a typed object back into
    markdown-with-frontmatter, so that round-tripping a read→write is lossless for known fields.
19. As a consuming layer, I want the zod schema to be the *only* place field types live, so that the
    YAML never carries type tags and a field's type can be changed in exactly one place.
20. As a consuming layer, I want mdwal to coerce a YAML value to the declared type on read (e.g. a
    stringy number into a number), so that hand-edited or legacy files still parse under the schema.
21. As a peer, I want each `events.log` line to have a fixed 4-field header
    `<ts_ns>::<op>::<entity_type|folder>::<entity_id|empty>` followed by a JSON payload, so that
    arbitrary user text (field values, author, snapshots) can never collide with the `::` separator.
22. As a consuming layer, I want mdwal to parse an `events.log` back into structured events, so that
    replay and purge can operate on typed events, not raw strings.
23. As a consuming layer, I want serialize(parse(log)) to be stable, so that reading and rewriting a
    log doesn't perturb it.
24. As a peer in a **remote** workspace, I want every operation to append exactly one line to
    `events.log`, so that my authored history is complete and shareable.
25. As a peer in a **local** workspace, I want operations applied directly to files with no
    `events.log` and no LWW, so that local-only use has zero coordination overhead.
26. As a consuming layer, I want each operation to accept a "log / don't log" choice, so that internal
    or migration operations can mutate a workspace without polluting the authored `events.log`.
27. As a migration author, I want to express a migration as a static mdwal log replayed once with
    logging **off**, so that a structural change every peer applies identically is never mistaken for
    a user-authored, LWW-subject event.
28. As a peer, I want entity **folders** (`tasks/`, `boards/`, …) created / renamed / truncated /
    deleted through mdwal folder operations, so that the set of entity types is managed by the same
    engine as their contents.
29. As a peer, I want the workspace root folder itself treated as just another folder subject to
    `RENAME_FOLDER` / `TRUNCATE_FOLDER`, so that renaming a workspace still produces a log line inside
    that same folder.
30. As a peer, I want folder-operation log lines to use the fixed sentinel `$root` in the folder-name
    position for the workspace root (never its current or previous name), so that replays with several
    renames in a row are unambiguous.
31. As a peer, I want `truncate` on an entity folder to be expressible as a folder operation, so that
    clearing all tasks is one recorded action rather than N deletions.
32. As a consuming layer, I want the set of "fixed entity folders" derived from the current schema, so
    that adding an entity type in a migration automatically updates what counts as a fixed folder
    (used later by the manager to regenerate `.gitignore`).
33. As a migration author (dev-time only — schema is embedded in the binary and never mutable at
    runtime; see ADR-0007), I want `field create <entity> <field-type>` to register a new field in the
    entity's zod schema, and I want an added field to materialize on read from its **zod `default`** with
    **no migration needed**, so that old `CREATE` snapshots lacking the field still parse.
34. As a migration author, I want `field rename <entity> <new-name>` to be a **replay transform keyed by
    version** (ADR-0007), remapping the field *key* (and its `_lastModified`/`_lastModifiedBy` companions)
    on old events during replay — **not** a one-shot rewrite of files — because events are immutable and
    still carry the old key, so a one-shot rewrite would be reverted by the next replay.
35. As a migration author, I want `field delete <entity>` to need **no explicit migration** — replay
    ignores an `UPDATE_FIELD` for a field absent from the current schema and zod strips the unknown key on
    read — so that removing a field is handled by the schema itself.
36. As a migration author, I want `field change-type` (non-coercible) to be a **code transform keyed by
    version** applied on replay (ADR-0007), not a one-shot rewrite of derived files, so that mdwal never
    silently guesses a coercion and the migrated value survives every later replay. (The dev writes the
    transform because they do not have the user's data; `field change-type` scaffolds it and marks the
    schema-change version.)
37. As a consuming layer, I want a `create` event to carry the complete initial snapshot of the entity
    (all schema fields), so that a peer receiving it can materialize the entity without any other event.
38. As a consuming layer, I want a purge primitive that removes this peer's own events with
    `ts <= threshold`, so that the manager's sync can drop already-consolidated events safely.
39. As a peer, I want purge to be safe by construction — because the log is monotonic, every event
    appended after a rebuild has `ts > threshold` — so that an unconsolidated event is never purged.
40. As a consuming layer, I want a replay primitive that applies a set of events over a base snapshot
    under LWW without re-logging them, so that the manager can rebuild derived state from `master` +
    the peer's not-yet-consolidated events.
41. As a peer, I want replay to be deterministic regardless of event arrival order (LWW decides by
    timestamp, not by position), so that convergence does not depend on how events were interleaved.
42. As a consuming layer, I want a `Repository<Model>` per entity type exposing
    `create/update/delete/recover/findById/findAll/filter`, so that higher layers work with model
    objects instead of raw mdwal operations.
43. As a consuming layer, I want `Repository.delete(id)` to perform a soft-delete and `recover(id)` to
    undo it, so that repository semantics match the domain's soft-delete rule.
44. As a consuming layer, I want `Repository.findAll()` to exclude nothing structurally (soft-deleted
    entities remain retrievable), so that callers decide whether to filter `deleted` themselves.
45. As a consuming layer, I want `Repository.filter(predicate)` to run an in-memory predicate over all
    materialized entities, so that list/query features can be built without leaking mdwal internals.
46. As a peer creating a task, I want the `board` field to default to the sentinel `"inbox"` when no
    board is given, so that every task belongs to a board without a real "inbox" board existing
    (ADR-0004).
47. As a consuming layer, I want the nanoid generator to never produce the literal `"inbox"`, so that
    the virtual-board sentinel can never collide with a real board id (ADR-0004).
48. As a peer, I want a task's date to live entirely inside the `rrule` field (`DTSTART` = date,
    `RRULE` = recurrence, `rrule: null` = undated), so that there is a single source of truth for
    "when" with no parallel `date`/`dueDate` field.
49. As a peer, I want each entity file named `<id>.md` where `<id>` is a stable 21-char nanoid minted
    once at creation, so that the filename never changes when content changes.
50. As a consuming layer, I want mdwal to expose the same read/parse/write API for `config.md` (schema-
    validated markdown+frontmatter) even though it lives outside any workspace and outside LWW, so that
    configuration reuses the engine without joining the event/LWW domain.
51. As a peer, I want every event to carry the **semver of the binary that generated it** in its payload,
    so that a later replay knows which schema the event's values were written under (ADR-0007). The binary
    embeds the `version → schema` map; schema is dev-authored and lives only in the binary.
52. As a consuming layer, I want `replay` to apply, in version order, every registered **data-migration
    transform** whose schema-change point is newer than an event's version before folding that event under
    LWW, so that an event authored under an older schema is materialized as if authored under the current
    schema — the log stays immutable and unmigrated while derived state is always current-schema (ADR-0007).
    Transforms are code shipped in the binary; they rewrite the value inside both `CREATE` snapshots and
    `UPDATE_FIELD` payloads, and any rewritten value inherits the original event's timestamp.
53. As a consuming layer, I want `replay` (and therefore `pull`/`sync`/`rebuild`) to **hard-refuse** any
    event whose version is **greater** than the running binary's version, surfacing an "update taskthing to
    ≥ `<version>`" error rather than reading future-schema data best-effort, so that an older binary can
    never silently corrupt data it cannot transform down (the version barrier — ADR-0007). z.coerce remains
    only a read-tolerance for hand-edited/legacy files within a single schema version, never a migration
    mechanism.

## Implementation Decisions

### Modules built

- **`mdwal` engine** — the low-level module. Public surface (names indicative, shapes normative):
  - **Reads:** `read(entityPath) -> ParsedEntity` (frontmatter→typed object via the bound zod schema,
    with coercion); `readAll(entityType)`; `parseLog(path) -> Event[]`.
  - **Writes / operations**, each atomic and each taking an explicit `{ log: boolean }` (or equivalent)
    flag deciding whether a line is appended to `events.log`:
    - `createEntity(entityType, snapshot)` → `CREATE` event carrying the full initial snapshot.
    - `updateField(entityType, id, field, value)` → `UPDATE_FIELD` event; the write updates the value
      **and** `<field>_lastModified` (from the event header ts) **and** `<field>_lastModifiedBy` (from
      payload `author`) in one atomic file write. Soft-delete/recover are just `updateField` on
      `deleted`.
    - Folder ops: `createFolder`, `renameFolder`, `truncateFolder`, `deleteFolder` — for entity folders
      and for the workspace root (root uses the `$root` sentinel in the folder-name header position).
    - Field-schema ops (**dev-time migration-authoring tools only**, never a runtime user capability —
      schema lives in the binary and is immutable at runtime, ADR-0007), classified by whether they affect
      entity-event replay: `fieldRename` and non-coercible `fieldChangeType` are authored as **code
      transforms keyed by version, applied on replay** (not one-shot derived rewrites; mdwal never infers
      coercion); `fieldCreate` is handled by the zod `default` on read and `fieldDelete` by
      ignore-unknown-on-replay/zod-strip — both need **no** transform.
  - **Transform registry (ADR-0007):** an ordered array `Transform[] = { version, up: (ev: Event) => Event }`
    embedded in the binary (`src/schema/transforms.ts`), where `version` is the semver at which that schema
    change shipped. On replay, for an event tagged `V`, mdwal applies — in ascending version order — every
    `up` whose `version > V` before folding under LWW; each `up` inspects `ev.entityType`/`ev.op`/field and
    rewrites the value/key inside the `CREATE` snapshot or `UPDATE_FIELD` payload, keeping the original
    event timestamp. Each `up` must be **pure and deterministic** (identical across every binary of that
    version) or peer convergence breaks. `field rename`/`field change-type` scaffold a stub entry.
  - **Consolidation primitives:** `purge(threshold_ns)` (drops this peer's events with `ts <= threshold`);
    `replay(baseSnapshot, events)` (applies events over a base under LWW, **without** re-logging).
  - **Serialization:** `serializeLog(Event[])`, `serializeEntity(object) -> markdown`.
- **`Repository<Model>`** — thin adapter over mdwal per entity type. Interface exactly as in CONTEXT:
  ```ts
  interface Repository<Model> {
    create(model: Model): Promise<Model | null>
    update(model: Model): Promise<boolean>
    delete(id: string): Promise<boolean>   // soft-delete → updateField(deleted, true)
    recover(id: string): Promise<boolean>  // → updateField(deleted, false)
    findById(id: string): Promise<Model | null>
    findAll(): Promise<Model[]>
    filter(predicate: (value: Model) => boolean): Promise<Model[]>
  }
  ```
- **Schema module** — the zod schemas are the single source of truth for field types. Ships `taskSchema`
  and `boardSchema` (verbatim from CONTEXT `models`), plus the machinery that, given an entity schema,
  derives the companion LWW field set (`<field>_lastModified`, `<field>_lastModifiedBy`) and the derived
  `createdAt` / `updatedAt`.
- **Timestamp source** — a per-peer monotonic clock: `next() = max(now_utc_ns, lastLocalTs_ns + 1)`,
  where `lastLocalTs_ns` is read from the last line of this peer's `events.log` (ADR-0006). Nanosecond
  precision (ADR-0001).
- **Author source** — reads global git `user.name`; required even for local workspaces (ADR-0002). A
  missing/unset git identity is a hard error surfaced to the caller.
- **Nanoid generator** — 21-char ids, with the invariant that `"inbox"` is never emitted (ADR-0004).

### Event / log contract

- Line format: `<timestamp_ns>::<op>::<entity_type | folder-name | $root>::<entity_id | "">::<payload_json>`.
- The 4-field header is fixed-arity; **all** free-form content (field values, author, full create
  snapshot) lives inside `payload_json` only — never in the header — so `::` inside user text can't
  collide.
- `payload_json` carries the **semver of the binary that authored the event** (ADR-0007), so replay can
  pick the right data-migration transforms. This version is metadata for migrate-on-read only; it never
  participates in LWW comparison (which stays pure timestamp-vs-timestamp, ADR-0001).
- `op` ∈ { `CREATE`, `UPDATE_FIELD`, `CREATE_FOLDER`, `RENAME_FOLDER`, `TRUNCATE_FOLDER`,
  `DELETE_FOLDER` } (exact spelling to be fixed during implementation but the set is closed and no op
  combines multiple actions).
- Events are **append-only and immutable**; a losing-LWW event stays in the log, just isn't applied.
- **Local** workspaces write no log; **remote** workspaces append exactly one line per logged operation.
  Local vs. remote is a property of the workspace, not of the file format — the entity/`_lastModified`
  metadata exists in both; only the presence of `events.log` differs.

### LWW / replay semantics

- Per-field, denormalized into the frontmatter (`<field>_lastModified` ns, `<field>_lastModifiedBy`).
- Cross-peer comparison is pure timestamp-vs-timestamp; exact ties (astronomically unlikely at ns
  precision) resolve arbitrarily but deterministically (ADR-0001). **Not** an HLC — no logical counter,
  no cross-peer causality (ADR-0006).
- `replay(base, events)` folds events by field, keeping the max-timestamp value per field, over the base
  snapshot; independent of event ordering.
- **Migrate-on-read (ADR-0007).** Before folding an event, `replay` applies every registered data-migration
  transform whose schema-change version is newer than the event's own version, in version order, rewriting
  the value in the `CREATE` snapshot / `UPDATE_FIELD` payload; the rewritten value keeps the original
  event's timestamp so purge/threshold reasoning (ADR-0006) is untouched. Transforms are code embedded in
  the binary (one per schema-change point). **Version barrier:** an event whose version is greater than the
  running binary's version aborts the replay with an "update taskthing" error — no best-effort read of
  future-schema data. `master` is stamped with the version it was built at; a binary older than `master`
  refuses `pull`, and `rebuild` refuses if any collected peer event is newer than the rebuilder's binary.
- `purge(threshold)` relies on log monotonicity: after a rebuild consolidates up to `threshold`, every
  later local event has `ts > threshold`, so purging `ts <= threshold` is safe (ADR-0006).

### Config-as-mdwal

- `config.md` is read/written through the same schema-validated markdown+frontmatter path, but lives
  outside any workspace and **outside** the LWW/`events.log` domain (direct write, like a local
  workspace). Included here only to the extent of the shared read/serialize path; the config *schema*
  and its keys are defined in a later config spec.

### Explicitly deferred contracts (named, not defined here)

- Manager git orchestration (init 1.1/1.2/1.3, sync 2.1/2.2, rebuild, `pull`, truncate-history) — a
  later spec. mdwal only provides the primitives it composes (`purge`, `replay`, read/parse).
- `migrations/` application-record folder, `.gitignore` regeneration, `kv_store`, porcelain commands,
  ink TUI, theming, distribution/auto-update — later specs. This spec only exposes the *derived set of
  fixed folders* the manager will consume.

## Testing Decisions

**What a good test looks like here.** Test **external behavior at the mdwal public API seam only** —
never private helpers, file-internal layout, or intermediate data structures. A test drives mdwal with
`(zod schema, a temp workspace directory, a sequence of operations)` and asserts on the two observable
outputs: the derived entity `.md` files (via `read`/`readAll`, i.e. typed objects) and the `events.log`
lines (via `parseLog`, i.e. typed events). Determinism/convergence tests assert that two different
operation interleavings that carry the same timestamps produce byte-identical derived state.

**Primary seam (committed by this spec): the mdwal public API.** Every core invariant is observable
here without reaching inside:

- **LWW per field / convergence:** two concurrent `updateField`s on the same field → higher ts wins;
  on different fields → both survive; replaying the same events in different orders → identical state.
- **Losing event still logged:** after a lost race, `parseLog` still contains the losing event; `read`
  shows it wasn't applied.
- **Monotonic timestamps:** simulate a backward wall clock (inject the clock) and assert the log stays
  strictly increasing and self-LWW stays consistent (later self-edit wins).
- **Atomic field write:** one `updateField` updates value + `_lastModified` + `_lastModifiedBy` together;
  never observable in a partial state.
- **Create snapshot:** a `CREATE` event's payload materializes the whole entity; `createdAt` = create ts;
  `updatedAt` = max field `_lastModified`.
- **Log format:** free-form values containing `::` round-trip through serialize/parse without header
  corruption; `$root` sentinel used for workspace-root folder ops.
- **Local vs. remote:** the same operation produces an `events.log` line in a remote workspace and none
  in a local one, with identical derived files.
- **Log on/off flag:** an operation run with logging off mutates files but appends no log line (migration
  path).
- **Purge + replay primitives:** purge drops only `ts <= threshold`; a post-threshold event is never
  dropped; `replay(base, events)` reproduces the LWW-resolved state without appending to the log.
- **Nanoid invariant:** the generator never returns `"inbox"`; default `board` on task create is `"inbox"`.
- **Migrate-on-read (ADR-0007):** with an injected transform registry, an event authored under an older
  version replays to the current-schema value (covering both `CREATE` snapshot and `UPDATE_FIELD`); the log
  line is unchanged (still old-typed); the rewritten value keeps the original timestamp. A coercible change
  needs no transform (z.coerce on read); a non-coercible one is wrong without the transform and correct with
  it. **Version barrier:** an event tagged with a version greater than the (injected) running version aborts
  replay with an update error and mutates nothing.

**Second seam: `Repository<Model>` public interface.** Tested through its seven methods against a temp
workspace (create→findById→update→findAll→filter→delete(soft)→recover), asserting soft-delete leaves the
entity retrievable and `deleted = true`.

**Modules tested:** mdwal engine (primary), `Repository<Model>` (secondary). rrule/date handling is
covered only at the schema level (a valid RRULE string parses, an invalid one is rejected by
`rruleSchema`); **recurrence-completion logic is out of scope**. Manager and CLI seams are **not**
introduced by this spec.

**Prior art:** none — this is a greenfield repo (`index.ts` is a stub). This spec therefore *establishes*
the test pattern: `bun test` (per `CLAUDE.md`), one test file per seam, temp directories via Bun APIs, an
injectable clock and an injectable author/git-identity source so timestamp-monotonicity and attribution
are testable without touching the real system clock or global git config.

## Out of Scope

- The `manager` and all git operations (init/sync/rebuild/pull/truncate-history, the `master` snapshot,
  the rebuild note object). mdwal only ships the `purge`/`replay`/read primitives they build on.
- Porcelain CLI commands and their argument syntax (`add` string-input parser, `list` filters, etc.).
- The ink TUI (spinners, selection/confirmation UIs, list rendering, themes) and ANSI theming (ADR-0005).
- `kv_store` number↔nanoid mapping and the ephemeral numbering used by `list`.
- Distribution: cross-platform binary build/workflow, semver, self-update.
- The migration *runner* / `migrations/` application-record folder and `.gitignore` regeneration
  (mdwal here only exposes the derived fixed-folder set; the manager consumes it later).
- Recurrence-completion domain logic (minting the next occurrence on `check`, advancing `DTSTART`).
- The concrete `config.md` schema and its keys (only the shared read/serialize path is in scope).

## Further Notes

- Language: CONTEXT.md and the ADRs are in Portuguese; this spec is in English to match the code/CLI
  language, while preserving the exact glossary terms (mdwal, workspace, peer, LWW, plumber/porcelain,
  sentinel `"inbox"`, `$root`).
- The plumber `entity` commands in CONTEXT are the *user-facing* surface of the mdwal operations specified
  here; defining those commands (and their `--log` flag ergonomics) belongs to the CLI spec, but they must
  map 1:1 onto the operations frozen above. The `field` commands, by contrast, are **dev-time
  migration-authoring tools, not a runtime user capability** (schema lives in the binary — ADR-0007).
- Injectable seams to build in from day one (they make the invariants testable and keep the API honest):
  the **clock** (for monotonic-timestamp tests) and the **author/git-identity source** (for attribution
  tests and to make the ADR-0002 git dependency explicit rather than ambient).
- ADR cross-references: LWW model — ADR-0001; author source — ADR-0002; events-only remote versioning &
  the derived fixed-folder set — ADR-0003; board sentinel & nanoid invariant — ADR-0004; ANSI theming
  (out of scope, referenced) — ADR-0005; per-peer monotonic timestamps — ADR-0006; migrate-on-read,
  versioned events, data-vs-structural migration split & the version barrier — ADR-0007.
