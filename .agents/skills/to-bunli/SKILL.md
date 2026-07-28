---
name: to-bunli
description: Migrate a frameworkless Bun/Node CLI to the Bunli framework — convert hand-rolled arg parsing, command dispatch, and help text into structured defineCommand modules with typed options, config, and codegen. Use when the user wants to adopt Bunli, restructure a CLI onto Bunli, or "convert this CLI to bunli".
---

Migrate a CLI that parses `argv` and dispatches by hand into a structured Bunli project. The job is a **faithful port**: every existing command, flag, and behaviour survives the move; only the plumbing changes.

Work through the phases in order. Don't scaffold new structure before you've mapped what exists — a port you can't check against the original is a rewrite in disguise.

[`bunli-api.md`](bunli-api.md) is the condensed API surface — `defineCommand`, `option`, handler context, config, validation, plugins, codegen. Consult it whenever you write Bunli code; don't reconstruct the API from memory.

## Phase 1 — Inventory the existing CLI

Read the current CLI end to end before changing anything. Produce a written inventory covering:

- **Entry point** — where `argv` is read and dispatched (the `switch`, the `if` chain, the parser call).
- **Every command and subcommand** — name, aliases, what it does, its handler location.
- **Every flag/option per command** — long name, short name, type, default, required-ness, and any coercion the code does by hand (`parseInt`, `=== "true"`, comma-splitting).
- **Positional arguments** per command.
- **Cross-cutting behaviour** — global flags, env-var reads, shared setup/teardown, custom help/version output, exit codes.

Completion criterion: a table or list where every command maps to its flags, positionals, and current behaviour. This inventory is the checklist you port against in Phase 4 and verify against in Phase 5.

## Phase 2 — Install and scaffold

Confirm Bun ≥ 1.0, then set up the Bunli skeleton (see [`bunli-api.md`](bunli-api.md) § Project shape):

- `bun add @bunli/core` and `bun add -d bunli`. Add a validation library if none is present — default to `zod` unless the project already uses valibot/typebox/arktype.
- Create `bunli.config.ts` with `name`, `version`, `description`, and `commands.directory`.
- Create the entry (`createCLI()` + `cli.run()`) and a `src/commands/` directory.
- Wire `package.json`: `bin` pointing at the entry, and `dev`/`build`/`test` scripts.
- Add `.bunli/commands.gen.ts` to `tsconfig.json`'s includes so generated types resolve.

Completion criterion: `bunli dev --help` runs and prints the CLI header, even with zero commands ported yet.

## Phase 3 — Design the command tree

Decide the shape before porting handlers. Map the inventory onto Bunli's model:

- Flat commands stay flat. Group related commands under a parent using nested `commands: [...]` when the original used a prefix convention (`db:migrate`, `db:seed` → a `db` group with `migrate`/`seed`).
- Carry every alias across. Preserve the exact command names users already type — Bunli's deepest-match resolution means `db migrate` and a top-level `db-migrate` are different; keep whichever the original exposed.
- Plan one file per top-level command under `src/commands/`.

Completion criterion: a file-by-file plan naming each command file and the command/subcommand tree it holds.

## Phase 4 — Port command by command

Port one command at a time, smallest first. For each, write a `defineCommand` module (patterns in [`bunli-api.md`](bunli-api.md) § Commands):

- `name` **must** end in `as const` — without it, handler flags lose their types.
- Turn every hand-parsed flag into an `option(schema, { description, short })`. Replace manual coercion with schema coercion: `parseInt` → `z.coerce.number()`, `"true"` checks → `z.coerce.boolean()`, comma-splits → `z.string().transform(s => s.split(","))` or `z.array(...)` with `repeatable: true`. Preserve defaults with `.default()` and required-ness by omitting `.optional()`.
- Move the handler body in, pulling from context instead of globals: `shell` for subprocess calls (replace `execa`/`child_process`), `env` for `process.env`, `prompt` for interactive input, `spinner` for progress, `positional` for non-flag args.
- Keep the handler's real work — its logic and side effects are ported verbatim; only input parsing and I/O helpers change.
- Register the command (`cli.command(...)`) or place it for directory discovery, per your Phase 2 wiring.

After each command: run it under `bunli dev <command> ...` with the same arguments the inventory recorded and confirm identical behaviour before starting the next. Delete the original command's dead code once its replacement passes.

Completion criterion: every command in the Phase 1 inventory has a `defineCommand` module and its old implementation is removed.

## Phase 5 — Verify the port, then clean up

- Diff against the inventory: every command, alias, flag, short flag, default, and positional from Phase 1 is present. Account for each one explicitly — an unported flag is a silent regression.
- Run `bunli build` (and `bunli test` if tests exist) and confirm it succeeds.
- Confirm codegen: `.bunli/commands.gen.ts` exists and typechecks; no handler carries a manual `{ flags }` annotation (that's the sign `as const` is missing).
- Remove the now-dead arg-parsing dependency (`commander`, `yargs`, `minimist`, etc.) from `package.json`.

Completion criterion: build passes, generated types resolve, the old parser dependency is gone, and the inventory diff is clean.
