# taskthing

A git-based task manager for your terminal — **no central server, no account, no coordination**.

taskthing keeps your tasks and boards as plain Markdown files. Point it at a git remote and any number of machines can share the same workspace and converge automatically: every change is an append-only event, and conflicts are resolved deterministically per-field by **Last-Write-Wins** (nanosecond timestamps). Work offline, sync when you want. It ships as a single self-contained binary for Linux, macOS and Windows, and updates itself.

---

# ⚠️ AI-Assisted Development

This project was developed entirely with AI assistance and carefully reviewed, validated, and curated by me.

It was **not** "vibe coded"." A significant amount of time, effort, and tokens were invested in designing specifications, validating behavior, reviewing implementations, and ensuring the final result meets the intended quality standards.

## Install

**Linux / macOS**

```sh
curl -fsSL https://raw.githubusercontent.com/costaluu/taskthing/master/scripts/install.sh | sh
```

**Windows (PowerShell)**

```powershell
irm https://raw.githubusercontent.com/costaluu/taskthing/master/scripts/install.ps1 | iex
```

The installer downloads the right binary for your OS/arch into a per-user location on your `PATH`, then tells you to finish setup:

```sh
taskthing install
```

`taskthing install` scaffolds `~/.config/taskthing/` with a local workspace and asks two quick questions: your date format (american `yyyy/mm/dd` or europe `dd/mm/yyyy`) and whether your terminal has [Nerd Font](https://www.nerdfonts.com/) glyphs.

---

## Quick start

```sh
taskthing add "walk the dog d:[tomorrow]"     # a dated task
taskthing add "standup r:[every weekday]"     # a recurring task
taskthing list                                # numbered list of open tasks
taskthing check 1                             # complete task #1
taskthing star 2                              # star task #2
taskthing board add "Work"                    # create a board
taskthing boards                              # list boards
```

The numbers (`1`, `2`, …) are ephemeral references handed out by the most recent `list`; they are **not** stable ids. Re-run `list` and they are renumbered.

---

## Concepts

- **Workspace** — a folder holding your `tasks/` and `boards/`. It can be **local** (files edited directly, no history) or **remote** (associated with a git remote — every change is logged to an append-only `events.log`, and the derived files are rebuilt from that log). You can have many workspaces and switch between them.
- **Task** — has a title, an optional board, star, description, completion, and a date/recurrence. There's no separate date field: the date _is_ the `DTSTART` of an [RFC 5545](https://icalendar.org/) `rrule`; the recurrence is its `RRULE`.
- **Board** — a named, coloured, icon-tagged bucket for tasks. Every task without a board lives in the virtual **inbox** board.
- **LWW / offline-first** — remote workspaces converge without a coordinator: each peer only ever writes its _own_ events; consistent snapshots are published to `master` by `rebuild`; concurrent edits to the same field are settled by the newest timestamp.
- **Migrations** — schema/structure changes ship _inside the binary_ and are applied once per workspace on update; the binary is the single source of truth for what exists.
- **Themes** — the whole TUI is themeable by remapping semantic roles to the 16 ANSI colours (no RGB) — so it always fits your terminal's palette.

### The `add` grammar

`add` reads natural-language date and recurrence out of the input via a strict bracket syntax (dates parsed with [chrono](https://github.com/wanasit/chrono), recurrence with [rrule](https://github.com/jkbrzt/rrule)):

| Tag                         | Meaning                                                |
| --------------------------- | ------------------------------------------------------ |
| `d:[…]` or `date:[…]`       | a one-off date (the `DTSTART`), e.g. `d:[next friday]` |
| `r:[…]` or `recurrence:[…]` | a recurrence rule, e.g. `r:[every monday]`             |

The brackets are required and the whole `tag:[…]` is stripped from the title. Text without closing brackets (`remind Ed: buy milk`) stays part of the title.

---

## Commands

`--workspace=<name>` is accepted almost everywhere and defaults to your current workspace. `<id>` is the ephemeral number from the last `list`.

### Tasks

| Command                        | What it does                                                                   |
| ------------------------------ | ------------------------------------------------------------------------------ |
| `add "<input>"`                | Create a task, parsing `d:[…]` / `r:[…]` out of the text                       |
| `list [filters]`               | List open tasks, numbered (see filters below)                                  |
| `check <id>` / `uncheck <id>`  | Complete / reopen a task (checking a recurring task mints its next occurrence) |
| `star <id>` / `unstar <id>`    | Toggle a task's star                                                           |
| `delete <id>` / `recover <id>` | Soft-delete / restore a task                                                   |
| `clear <id>`                   | Permanently remove an _already soft-deleted_ task from this machine            |
| `set title <id> <value>`       | Retitle a task                                                                 |
| `set description <id> <value>` | Set a task's description                                                       |
| `set date-time <id> <value>`   | Rewrite the schedule (an empty value clears it)                                |
| `set board <id> <ref>`         | Move a task to a board by name, number or `inbox`                              |

**`list` filters:** `--checked` (include completed), `--starred`, `--deleted`, `--hasDescription`, `--period=<Nd|Nm|Ny>` (within a window from now), `--in-board=<name>,…` (or `--in-board=inbox`).

### Boards

| Command                                    | What it does                                                 |
| ------------------------------------------ | ------------------------------------------------------------ |
| `board add "<name>" [--icon=<glyph>]`      | Create a board                                               |
| `board list` (or the `boards` shortcut)    | List boards, numbered                                        |
| `board set name <id> <new-name>`           | Rename a board                                               |
| `board set icon <id> <icon>`               | Set a board's icon                                           |
| `board set color <id> <ansi-color>`        | Set a board's colour (ANSI 16)                               |
| `board delete <id>` / `board recover <id>` | Soft-delete / restore a board (its tasks fall back to inbox) |

### Workspaces

| Command                                         | What it does                                                              |
| ----------------------------------------------- | ------------------------------------------------------------------------- |
| `workspace list` (or the `workspaces` shortcut) | List your workspaces                                                      |
| `use <name>` (alias for `workspace use`)        | Switch the current workspace                                              |
| `workspace create\|rename\|delete <name>`       | Manage workspaces (`delete` is confirmed and can't remove the active one) |
| `workspace remote add <url>`                    | Associate a git remote (makes the workspace remote)                       |
| `workspace remote remove`                       | Detach the remote (remote content is preserved)                           |

### Sync (git manager)

| Command                              | What it does                                                |
| ------------------------------------ | ----------------------------------------------------------- |
| `sync`                               | Publish your events and pull the latest consolidated state  |
| `pull`                               | Re-derive local state from `master` without publishing      |
| `rebuild`                            | Consolidate every peer's log into a fresh `master` snapshot |
| `truncate --keep=<n> [--branch <b>]` | Bound a branch's history to its last `n` commits            |

### Config & themes

| Command                    | What it does                                                                   |
| -------------------------- | ------------------------------------------------------------------------------ |
| `config`                   | Open the interactive settings TUI (piped, prints the current settings instead) |
| `config set <key> <value>` | Set a setting non-interactively (scriptable)                                   |
| `theme`                    | Open the theme editor (default mapping vs. custom ANSI JSON)                   |

Configurable keys: `dateFormat` (`america`/`europe`), `nerdfont`, `theme`, `autoUpdate` (`confirm`/`silent`). The current workspace is changed **only** through `use` / `workspace use`.

### Updates & migrations

| Command        | What it does                                                                                                           |
| -------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `update check` | Check GitHub for a newer release (cached for 12h)                                                                      |
| `update apply` | Download and swap in the new binary, then run pending migrations (no prompt — running the command is the confirmation) |
| `migrations`   | Show which migrations have been applied to this workspace                                                              |

### Plumber (advanced / dev)

Low-level primitives that operate on full ids without the ephemeral numbers or confirmations — mostly used to author migrations:

| Command                                                   | What it does                                                                                                                                       |
| --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `entity create\|rename\|delete <name> [<new-name>] --log` | Print the mdwal log line for a fixed-folder structural migration (a dev-time authoring tool; the output is embedded in the binary's migration set) |

---

## Configuration & data layout

Everything lives under `~/.config/taskthing/`:

```
~/.config/taskthing/
  config.md              # global settings (Markdown + YAML frontmatter)
  <workspace>/
    tasks/  <id>.md
    boards/ <id>.md
    events.log           # remote workspaces only — the append-only source of truth
    .taskthing/          # local, gitignored: kv stores, migration records
```

`config.md` and every entity file is Markdown with YAML frontmatter — human-readable and git-friendly. In a remote workspace only `events.log` is versioned; the derived `tasks/`/`boards/` folders are rebuilt from it and stay gitignored.

---

## Build from source

taskthing is written in TypeScript and built with **[Bun](https://bun.com)**.

```sh
bun install
bun test                                             # run the test suite
bun src/index.ts <command>                           # run without compiling
bun run scripts/build.ts                             # compile a binary for this machine
bun run scripts/build.ts all                         # compile every released target
```

Release binaries are cut by CI on a semver tag (`git tag v1.2.3 && git push --tags`) — one per OS/arch, attached to the GitHub release, which is what `update` and the installers download.

---

## Status

taskthing is under active development. See [`CONTEXT.md`](CONTEXT.md) for the domain model, [`docs/specs/`](docs/specs/) for the layered specifications, and [`docs/adr/`](docs/adr/) for the architectural decisions. XD
