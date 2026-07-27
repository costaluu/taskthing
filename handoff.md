# Handoff — taskthing: spec 0005 (distribution/update/migrations), two seams done + CLI wired

**Repo:** `/home/reit-285375/projetos/taskthing-new` (branch `master`).
**Date:** 2026-07-23. **Model of work:** strict TDD (red→green), Bun-only, PT-BR conversation / English code.

## Read these first (do not re-derive)

- [`CONTEXT.md`](/home/reit-285375/projetos/taskthing-new/CONTEXT.md) — domain, ubiquitous language, command surface (PT).
- [`docs/specs/0001..0005`](/home/reit-285375/projetos/taskthing-new/docs/specs) — layered specs. **This session = spec 0005.**
- [`docs/adr/0001..0007`](/home/reit-285375/projetos/taskthing-new/docs/adr) — frozen decisions. ADR-0007 governs the migrate-on-read/version-barrier split.
- The **previous** handoff (specs 0001–0003) and the git history. Last commit is `6f009db adr0004` — **all of spec 0004 (TUI/theming) is committed.**
- `.claude` project memory `MEMORY.md` — three entries, still relevant.

## Test/typecheck baseline

`bun test` → **160 pass, 0 fail** across 25 files. `bunx tsc --noEmit` clean apart from the **6 pre-existing** `'ev' is possibly undefined` warnings in the first test of `src/mdwal.test.ts` (filter with `grep -vE "mdwal.test.ts\(6[3-8]"`). Run one file: `bun test src/<name>.test.ts`.

## What this session did (spec 0005) — ALL UNCOMMITTED

`git status` since `6f009db`:

- **New:** `src/updater.ts` + `.test.ts`, `src/migration-runner.ts` + `.test.ts`, `src/build.ts`.
- **Modified:** `src/cli.ts`, `src/schema.ts`, `src/tui.tsx`, `src/cli.test.ts`.

Two committed seams of spec 0005 are implemented and the CLI is wired to them.

### Seam 1 — update orchestrator (`src/updater.ts`, pure, injected boundaries)

`createUpdater(deps)` → `{ check, apply }`. All boundaries injected (`releaseSource` / `downloader` / `fs` / `clock` / `config`), so **no test touches network or a real binary**. Covered: semver compare via `Bun.semver.order`, 12h cache + persist, OS/arch asset selection (missing → clean error), apply gating (confirm default / silent / declined), download-then-swap atomicity (failed download leaves old binary), up-to-date short-circuit. `check`-only deps are required; apply-only deps (`downloader`/`fs`/`runMigrations`) are optional in the type and asserted at call.

### Seam 2 — migration runner (`src/migration-runner.ts`, structural only, real temp workspaces)

`createMigrationRunner({ migrations, workspaces, engine })` → `{ run }`. Covered: pending detection (embedded − recorded, version-ordered), apply folder-op log with **logging OFF** (`{log:false}`, nothing appended to `events.log`), record frozen `<version>.md` under `.taskthing/migrations/`, idempotency, `.gitignore` regeneration for **remote** workspaces only (heuristic: regenerate iff a `.gitignore` already exists), all-workspaces, and per-workspace failure that is surfaced + **not recorded** + does not block other workspaces (collect-and-aggregate).

### CLI wiring (`src/cli.ts`)

- `migrations` → union(embedded, recorded) rows; `MigrationList` on TTY, plain text piped.
- `update check` / `update apply` → real `buildUpdater()` (GitHub `releases/latest`, `fetch` downloader, swap over `process.execPath`). check uses the plain `Spinner` (dynamic success line); apply gates on `autoUpdate` + `runConfirmation`.
- `tui.tsx` gained `renderSpinner` and `runConfirmation`.
- `schema.ts` config gained binary keys: `lastUpdateCheck` / `updateAvailable` / `updateTarget` / `autoUpdate` (all defaulted; `autoUpdate` added to `CONFIG_KEYS`).
- `build.ts` holds `VERSION = "0.1.0"` and `MIGRATIONS: Migration[] = []` (empty until a migration ships; the binary is the source of truth).

## Decisions made this session that are NOT in the specs

- **Migration records live at `.taskthing/migrations/<version>.md`** (not a top-level `migrations/`), so the existing `.gitignore` (`.taskthing/`) covers them for free (ADR-0003), same as kv stores.
- **`.gitignore` regeneration is gated on a `.gitignore` already existing** — the runner never invents one for a local workspace.
- **Migration content is authored as a plumber log in tests**: a real remote-mode mdwal `createFolder(...)` writes `events.log`, whose text becomes the migration `content`.
- **Config-migration in the runner was deliberately deferred** (folder-ops first — user decision). Still TODO.
- **migrate-on-read / version barrier (ADR-0007) deferred** to a separate mdwal effort (user decision). NOTE: `src/mdwal.ts` **already has partial machinery** — `compareSemver`, `migrateEvent`, `Transform` — investigate before building.
- **`update` non-network paths are the only spawn-tested ones**: `update check` cached path and `update apply` up-to-date path (both seeded via a fresh `lastUpdateCheck` in config.md, so `releaseSource` is never called). The pending→download→swap path and the real GitHub fetch are **untested glue**.
- **`TASKTHING_REPO` env** overrides the release repo (default `costaluu/taskthing` — placeholder, confirm the real owner/repo before release).

## Gotchas found the hard way (this session + carried)

- `await expect(shellPromise).rejects.toThrow()` **hangs** with Bun `ShellPromise`; for rejects, use `expect(p).rejects.toThrow(...)` **without `await`** then `await new Promise(r=>setTimeout(r,20))` before follow-up fs assertions (used in updater/migration-runner tests).
- **Binary swap over `process.execPath` is only safe for the compiled binary.** Under `bun cli.ts` (dev/tests) `execPath` is bun itself — never reached in tests, but do not exercise the real apply-download path in dev.
- ink needs a **TTY/raw mode** for `useInput` components (Confirmation/Selection/TextInput/interactive screens); the spawn harness is non-TTY, so interactive wirings are covered by **component tests**, not spawn tests. Non-interactive renders + spinners work fine non-TTY.
- CLI stays `.ts` (entrypoint `cli.ts`, spawned as `bun cli.ts`); all JSX lives in `src/tui.tsx`. Do not rename `cli.ts` → `.tsx` (breaks `cli.test.ts` `ENTRYPOINT`).
- TUI is gated on `process.stdout.isTTY`; piped output stays plain so existing spawn tests keep asserting `N title`-style text.

## Suggested skills / next steps

- **`/code-review`** — the spec 0005 files (`updater.ts`, `migration-runner.ts`, `build.ts`) and the `cli.ts` wiring have not been reviewed. Fixed point: `HEAD` (`6f009db`). New files need `git add -N` to appear in the diff (undo with `git reset`).
- **`/tdd`** — to continue. Natural next seams, each confirm before writing tests:
  1. **Config migration in the runner** (deferred): a structural migration rewriting a `config.md` value directly (config is not replayed from a log). Testing decision in spec 0005 §"config migration".
  2. **migrate-on-read / version barrier** in mdwal (ADR-0007) — the largest remaining piece; partial machinery already in `mdwal.ts`.
- **`domain-modeling`** — only if a new ADR is warranted (e.g. the `.taskthing/migrations/` record location, or the `.gitignore`-exists heuristic — neither is in an ADR yet).
- **Not unit-test seams (spec says so):** the CI release workflow (`bun build --compile` per OS → GitHub release assets) and the bootstrap `scripts/install.sh` / `install.ps1`. Build when ready; verify manually / by a smoke job.

## Working agreements (unchanged)

- Bun only (`bun test`, `Bun.file`, `Bun.write`, `Bun.$`, `Bun.semver`). `node:fs/promises` only for what Bun lacks.
- Red before green; one seam, one test, one minimal implementation. **Say so when a test passes without going red** (happened often when a whole state machine lands in the first green — flag it, keep the test as a regression guard).
- Confirm the seam with the user before writing any test at a new seam. User works decision-by-decision, in PT-BR.
- **Nothing is committed since `6f009db`.** Ask before committing; never push unprompted. End commit messages with the `Co-Authored-By: Claude Opus 4.8` trailer.

## Notes

- No secrets/PII in the repo. Tests write only to OS temp dirs.
- No new dependencies this session (used built-in `Bun.semver`, `fetch`).
