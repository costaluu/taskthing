# Spec 0004 — ink TUI rendering and ANSI-16 theming

> **Scope note.** This spec covers the **presentation layer**: how taskthing draws to the terminal
> with **ink**, and the **ANSI-16 theme** (semantic role → ANSI index 0–15) that colors everything.
> It sits on top of the porcelain commands from [Spec 0003](./0003-porcelain-cli-kvstore.md) — those
> commands produce plain data and consume returned values; **this spec draws that data and gathers
> interactive input**. It does not re-decide command behavior, parsing, kv_store, or git.
>
> Source of truth: [`CONTEXT.md`](../../CONTEXT.md) (`configuração e temas`, `interfaces (TUI)`
> sections) and [`docs/adr/`](../adr). The governing decision here is **ADR-0005**: the theme is
> strictly a remapping of semantic role → ANSI index (0–15); taskthing never emits RGB/true-color, and
> there are no named presets — only one **default** role→index mapping and a **custom** override.
>
> **Nerdfont dependency.** Nearly every visual element has a nerdfont glyph and a no-nerdfont fallback.
> Which variant is used is read from the user's `nerdfont` config (Spec 0003 `install`/`config`),
> defaulting to no-nerdfont.

## Problem Statement

The porcelain layer (Spec 0003) knows *what* to show (an ordered list of tasks, a board, a parsed
recurrence to confirm, a set of date-format choices) and *what* to collect (a y/n answer, a selected
option, a custom-theme JSON), but it deliberately produces and consumes **plain data** — it draws
nothing. Several things are still missing before taskthing is usable:

- **Async commands give no feedback.** `install`, `update check/apply`, `sync`, `rebuild`, `pull`, and
  `truncate` are inherently slow; a user staring at a frozen terminal can't tell success from hang.
- **Listings are unreadable as plain text.** A task line carries a lot of meaning — completion state,
  a date whose *color and phrasing* encode overdue-ness, a star, a recurrence marker, the board it
  belongs to — that only lands with deliberate styling.
- **Interactive edits have no surface.** Confirmations (destructive workspace/remote ops, recurrence
  parsing), finite-option selection (date format, theme mode), and free-text entry (`set` values,
  custom-theme JSON) need real terminal UIs.
- **Colors must respect the user's terminal.** taskthing wants to inherit whatever palette the user
  already has in their emulator (Dracula, Solarized, …) rather than fight it — which rules out RGB and
  mandates ANSI-index-only theming (ADR-0005).

## Solution

Build the **TUI layer** with ink, and the **theme system** underneath it.

- **Theme = role→ANSI-index map.** A fixed set of semantic roles (`highlight`, `error`,
  `text-secondary`, `success`, `warning`, `inbox-accent`, danger shades, board-date roles, …) each map
  to one ANSI index 0–15. taskthing emits only the index; the terminal resolves the actual color. There
  is exactly one **default** mapping (hardcoded, works on most terminals by ANSI convention) and a
  **custom** override supplied as role→index JSON. No RGB, ever; no named presets (ADR-0005).
- **Nerdfont-aware glyphs.** Every glyph has a nerdfont form and a no-nerdfont fallback, selected by the
  `nerdfont` config flag.
- **Three reusable UI primitives** driving every interaction: a **spinner** (async status →
  success/failure line), a **selection UI** (mono/multi-select, result always an array), and a
  **confirmation UI** (y/n). Plus **list renderers** for tasks, boards, and migrations, and a
  **text-input/textarea** for `set` values and the custom-theme JSON.
- **Composed screens** for the specific commands: the `install` first-run form, the `config` TUI, the
  `theme` shortcut, the async-command spinners, and the three list views.

Everything is themed — including the config/theme UIs themselves — through the same role→index system.

## User Stories

Actors: **user** (interacting at the terminal), **new user** (first-run `install`).

### Theme system (ANSI-16, role→index)

1. As a user, I want every colored element chosen by a **semantic role** mapped to an ANSI index (0–15),
   never an RGB value, so that taskthing inherits my terminal's existing palette (ADR-0005).
2. As a user, I want a single sensible **default** role→index mapping that works out of the box on most
   terminals (following the common ANSI convention: 1≈error/red, 2≈success/green, 3≈warning/yellow,
   4≈highlight/blue), so that I never have to configure the theme to get a good look.
3. As a user, I want a **custom** theme option where I paste role→index JSON (e.g.
   `{ "highlight": 4, "error": 1, "text-secondary": 8 }`), so that I can tweak specific roles without
   RGB and without a preset.
4. As a user, I want the custom-theme JSON validated (keys are known roles, values are integers 0–15),
   so that an invalid mapping is rejected with a clear message rather than silently breaking rendering.
5. As a user, I want there to be **no named presets** (Catppuccin, Gruvbox, …), so that the choice stays
   simply default-vs-custom (ADR-0005).
6. As a user, I want the theme applied to the config/theme TUIs themselves, so that the whole app is
   visually consistent, including the screens where I edit the theme.
7. As a user, I want the virtual-inbox accent to be its own role (`inbox-accent`) with a hardcoded default
   index, so that inbox stands out via the same role→index mechanism, not an RGB exception (ADR-0005).

### Nerdfont vs. no-nerdfont

8. As a user, I want every glyph to have a nerdfont form and a no-nerdfont fallback, chosen by my
   `nerdfont` config, so that the UI reads correctly whether or not my terminal has nerdfonts.
9. As a user without nerdfonts (the default), I want plain-unicode fallbacks (`[ ]`/`[x]`, `→`, `○`/`●`,
   `📬`, `⭐`, `🔄`, `⣾`, `✓`, `𐄂`, `ℹ️`, `⚠️`), so that nothing renders as tofu.

### Async command feedback (spinner)

10. As a user running an async command (`install`, `update check`, `update apply`, `sync`, `rebuild`,
    `pull`, `truncate`), I want an animated spinner with an in-progress message, so that I know work is
    happening.
11. As a user, I want the spinner to resolve into a **success** line (✓ / nerdfont check) or a **failure**
    line (𐄂 / nerdfont cross) with a summarized error message, so that every async command ends in a clear
    terminal state.
12. As a user, I want each command's messages to match CONTEXT's copy, parameterized by the relevant name
    (e.g. `syncing <workspace>…` → `<workspace> synchronized!`; `updating taskthing…` → `taskthing updated
    to version <target>`; update-check's three outcomes: latest / pending `<current> → <target>` / error),
    so that feedback is specific, not generic.
13. As a user, I want failure lines to show a **summarized** error (not a raw stack trace), so that the
    outcome is legible.

### List rendering — tasks

14. As a user, I want a listing headed by a small, bold, background-accented **"Tasks"** title, so that
    the view has a clear, characterful header.
15. As a user, I want tasks grouped under their **board** header — the board rendered underlined + bold in
    the board's own color, with its icon and name — so that I see which board each group belongs to.
16. As a user, I want the **inbox** board shown with its `inbox-accent` role (nerdfont inbox glyph, else
    `📬`), so that the virtual board is visually distinct.
17. As a user, I want each task line numbered (the number from the tasks `kv_store`), so that I can act on
    it by number.
18. As a user, I want a **check-mark** per task — `[ ]`/`[x]` without nerdfonts; with nerdfonts an unchecked
    box in a secondary color and a checked box in a success color — so that completion state is obvious.
19. As a user, I want a task's **date** rendered as colored, underlined text whose phrasing and color encode
    its temporal bucket:
    - before yesterday → `"N days ago"` in a dark danger color;
    - yesterday → `"yesterday"` in a lighter danger color;
    - today → `"today"` in a standout highlight (yellow);
    - tomorrow → `"tomorrow"` in a standout highlight (blue);
    - later this year → `"<day> <month>"` (e.g. `21 aug`) in a low-importance secondary color;
    - within a year but next year → `"<month> <year>"` secondary;
    - more than a year out → `"<year>"` secondary,
    so that overdue-ness and near-future dates are readable at a glance.
20. As a user, I want the **asymmetry** — past uses relative counting (`N days ago`) to convey lateness,
    future (beyond tomorrow) uses absolute dates for planning — so that each direction serves its purpose.
21. As a user, I want a **completed** task's date rendered differently: full completion date/time (the
    `completed` field's `lastModified`) in a plain secondary color (gray) with underline, in my configured
    date format — never the rrule — so that done items show when they were actually finished.
22. As a user, I want a **star** shown only when the task is starred (`⭐` without nerdfonts, a yellow
    nerdfont star with), and nothing when unstarred, so that starred items pop without clutter.
23. As a user, I want the **task title** in plain text, so that it stays readable next to the styled marks.
24. As a user, I want a **recurrence** marker on recurring tasks (`🔄` without nerdfonts, a distinct
    nerdfont glyph with, in a distinct highlight color), so that repeating tasks are identifiable.
25. As a user, I want the task line laid out as `<check> <date> <star> <title> <recurring>` under its board
    header, so that the ordering is consistent and scannable.

### List rendering — boards

26. As a user, I want a listing headed by a small, bold, background-accented **"Boards"** title.
27. As a user, I want each board numbered (from the boards `kv_store`) and rendered underlined + bold in its
    own color with its icon and name, so that boards are distinguishable and actionable by number.
28. As a user, I want inbox shown with its `inbox-accent` role here too, so that the virtual board is
    consistent across views.

### List rendering — migrations

29. As a user, I want a listing headed by a small, bold, background-accented **"Migrations"** title.
30. As a user, I want each applied/known migration listed as `<version> <applied?>` (yes/no), numbered from
    the migrations `kv_store`, so that I can audit migration state.
31. As a user, I want a closing **validation message** — "no migrations pending" (info) or "there's pending
    migrations, verify your taskthing installation" (warning), with nerdfont/`ℹ️`/`⚠️` variants — so that I
    immediately know whether the workspace is fully migrated.

### Confirmation UI

32. As a user, I want a confirmation UI (small standout title, a selector glyph, the question in a primary
    color ending in `?`, a `(y/n)` prompt with a placeholder) for destructive/important actions, so that I
    consciously approve them.
33. As a user, I want confirmation on **workspace delete**, so that I don't wipe a local folder by accident.
34. As a user, I want confirmation on **remote add** in the destructive init cases (1.2 preserve-and-log,
    and especially 1.3 which permanently deletes local content), so that associating never loses data
    silently — 1.3 has no bypass flag (the requirement is fixed in Spec 0002; this spec draws it).
35. As a user, I want the **recurrence confirmation** on `set date-time` (and recurring `add`) to show the
    *parsed* result, not a generic prompt — e.g. `"every monday, starting tomorrow (23 jul) — confirm?"` —
    so that I catch a parsing mistake before saving (the parsed string comes from Spec 0003).

### Selection UI

36. As a user, I want a selection UI for finite choices, rendering each item with a selected/unselected
    glyph (`○`/`●`, or nerdfont equivalents) and a current-item selector (`→`/nerdfont), the current item
    recolored, so that choosing is clear.
37. As a user, I want selection to support **mono-select** (one choice) and **multi-select**, with the
    result **always an array** (mono = a one-element array), so that callers have a uniform return shape.
38. As a user, I want mono-select used for the **date format** choice (american `yyyy/mm/dd` vs european
    `dd/mm/yyyy`) and the **theme mode** (default vs custom) and **board color** (one of the 16 ANSI names),
    so that finite options are picked, not typed.
39. As a user, I understand **multi-select** is currently speculative — no shipping command uses it (date
    format, nerdfont, theme are all mono/confirm) — but I want it in the design for a future need.

### Text input / textarea

40. As a user, I want a text-input/textarea for **`set` values** (`title`, `description`, `date-time`) and
    for the **custom-theme JSON**, so that free-form edits have a real surface.
41. As a user, I want the custom-theme textarea to feed the theme validator (roles known, indices 0–15), so
    that I get immediate, clear feedback on a bad mapping.

### First-run form (install)

42. As a new user, I want `install` to present an intuitive form: a **mono-select** for date format and a
    **y/n confirmation** for nerdfont support (default no), so that first-run setup is guided.
43. As a new user, I want the values I pick persisted to `config.md` (via Spec 0003's `install` behavior),
    so that later rendering/parsing respects them — this spec only draws the form and returns the values.

### Config / theme screens

44. As a user, I want `taskthing config` (no args) to open an interactive TUI navigating every configurable
    key **except** the current workspace (date format, nerdfont on/off, theme), so that I can adjust config
    visually; the current workspace is changed only via `use` (Spec 0003).
45. As a user, I want `taskthing theme` as a first-class shortcut jumping straight to the theme part
    (default-vs-custom, with the custom textarea), so that theming is one command.
46. As a user, I want the theme editor to offer exactly **default** or **custom** (textarea JSON), matching
    ADR-0005, so that there's no preset list to wade through.

## Implementation Decisions

### Modules built

- **Theme module** — the single source of the role→index mapping. Ships the **default** map (hardcoded,
  including `inbox-accent`) and a **custom** loader that parses/validates role→index JSON (known roles;
  integer indices 0–15). Exposes a resolver `color(role) -> ansiIndex` used by every renderer. **Never**
  emits RGB (ADR-0005). Roles include at least: `highlight`, `error`, `success`, `warning`,
  `text-secondary`, `inbox-accent`, the date buckets' danger/highlight/secondary shades, and board-title
  styling. (Exact role catalog finalized in implementation; the *contract* is role→index-only.)
- **Glyph module** — every glyph as a `{ nerdfont, fallback }` pair, resolved against the `nerdfont`
  config flag. Covers: spinner frames (`⣾…`), success/failure (`✓`/`𐄂`), check-marks (`[ ]`/`[x]`),
  inbox (`📬`), star (`⭐`), recurrence (`🔄`), selectors (`→`), select dots (`○`/`●`), info/warn
  (`ℹ️`/`⚠️`).
- **UI primitives (ink components):**
  - **Spinner** — animated frame + in-progress message → resolves to a themed success or failure line
    with a summarized error. Driven by an async operation from Spec 0003/0002/0005.
  - **Selection** — mono/multi; renders current-item selector + per-item selected dot; **returns an
    array** always (mono → length 1).
  - **Confirmation** — small standout title, selector, primary-colored question ending `?`, `(y/n)` input
    with placeholder; returns a boolean.
  - **TextInput/Textarea** — free-form entry; the textarea variant wires to the theme validator for custom
    JSON.
- **List renderers** — `renderTasks`, `renderBoards`, `renderMigrations`, each taking the plain data from
  Spec 0003's `list`/`boards`/`migrations` and drawing the titled, numbered, themed layouts (task line =
  `<check> <date> <star> <title> <recurring>` grouped by board header).
- **Date formatter** — maps a task's temporal state to the bucketed phrase + role per story 19/20/21,
  honoring the configured date format for absolute/completed dates.
- **Composed screens** — the `install` form, the `config` TUI (all keys except current workspace), the
  `theme` shortcut, and the async-command spinner wiring for `install`/`update`/`sync`/`rebuild`/`pull`/
  `truncate`.

### Rendering contract with the porcelain layer

- Porcelain commands (Spec 0003) remain the source of *what*: they hand this layer plain data (entity
  lists, parsed-recurrence strings, choice sets) and receive back plain values (selected array, boolean,
  entered text). This layer performs no command logic, no parsing, no kv_store, no git — it only draws and
  collects. This keeps the CLI seam (Spec 0003) testable without a terminal and this layer testable via
  ink's test renderer.

### Copy & buckets fixed by CONTEXT

- Spinner message templates for all seven async commands (in-progress / success / failure), the three
  update-check outcomes, and the migrations validation messages use CONTEXT's exact wording,
  parameterized by name/version/workspace/branch.
- The task-date buckets and their roles (danger-dark / danger-light / highlight-yellow / highlight-blue /
  secondary) and the past-relative vs. future-absolute asymmetry are fixed as in story 19/20.

### Deferred / consumed elsewhere

- The `nerdfont` and date-format **values** and the persisted custom-theme JSON live in `config.md` and
  are read/written by Spec 0003; this layer only *reads* them to pick glyphs/format and *returns* the
  values a form collects.
- Board color as an ANSI-16 name (selection UI) is drawn here; the write is Spec 0003.

## Testing Decisions

**What a good test looks like here.** Test **rendered output and interaction behavior at the ink-component
seam**, using ink's test renderer (e.g. `ink-testing-library`) — assert on the **frame text/structure and
the value a component returns**, not on private component state. Because the theme emits ANSI **indices**
(not RGB), tests can assert which semantic role (hence index) a segment uses via the theme resolver,
keeping color assertions deterministic and terminal-independent. Feed renderers the same plain data shape
Spec 0003 produces (fixtures), so rendering is tested in isolation from command logic.

**Primary seam (committed): the ink components / renderers.** Behavioral cases:

- **Theme:** default map resolves every role to a 0–15 index; a valid custom JSON overrides only the given
  roles; an invalid custom JSON (unknown role or index out of 0–15) is rejected; **no code path emits
  RGB** (assert output carries indices/named ANSI only).
- **Glyph selection:** with `nerdfont: false`, renderers use the fallback glyphs; with `true`, the nerdfont
  forms — asserted for check-marks, inbox, star, recurrence, selectors, spinner, info/warn.
- **Spinner lifecycle:** in-progress frame shows the parameterized message; on resolve → success line;
  on reject → failure line with a summarized error; per-command copy matches (sync/rebuild/pull/truncate/
  install/update-check's three outcomes/update-apply).
- **Task line:** given a fixture task, output is `<check> <date> <star> <title> <recurring>` under the
  correct board header; star omitted when unstarred; recurrence marker only when recurring.
- **Date buckets:** fixtures at `-3d`, `-1d`, `today`, `+1d`, later-this-year, next-year, `>1y` produce the
  right phrase **and** the right role; a completed task shows the completion timestamp in the configured
  date format with the secondary role (not the rrule).
- **Board / migrations views:** titled, numbered, board colored + underlined; inbox uses `inbox-accent`;
  migrations show `<version> <applied?>` and the correct pending/none validation message.
- **Selection UI:** mono returns a one-element array; multi returns the selected subset; current-item
  selector and selected dots render correctly.
- **Confirmation UI:** y → true, n → false; renders the parsed-recurrence question verbatim for
  `set date-time`; the init-1.3 confirmation exposes no bypass path.
- **Install form:** collects date format (mono-select) and nerdfont (y/n, default no) and returns them.

**Modules tested:** the theme module, glyph module, UI primitives, and list/date renderers — through the
ink-component seam. Command logic (Spec 0003), git (0002), and mdwal (0001) are **not** re-tested here;
renderers are fed fixture data.

**Prior art:** none in-repo for ink specifically (greenfield); this spec establishes the ink-testing
pattern (`ink-testing-library` under `bun test`). Reuse the injectable **"now"** from Spec 0003 so date
buckets are deterministic, and read `nerdfont`/date-format from an injected config so glyph/format tests
don't depend on a real `config.md`.

## Out of Scope

- All command *behavior*, parsing, `kv_store`, filtering, and wiring — Spec 0003 (this layer only draws
  the data those commands produce and returns collected input).
- Git orchestration (Spec 0002) and mdwal/LWW (Spec 0001).
- Distribution/auto-update mechanics and the migration runner — Spec 0005 (this layer draws their
  spinners/messages; the operations themselves live there).
- Persisting config values (date format, nerdfont, custom-theme JSON) — Spec 0003 owns `config.md` writes.
- Any RGB/true-color rendering or named theme presets — explicitly excluded by ADR-0005.
- Reflow/responsive behavior for extreme terminal widths beyond what ink handles by default (not specified
  in CONTEXT).

## Further Notes

- Language: English, matching code/CLI, preserving glossary terms (role→index, `inbox-accent`,
  nerdfont/no-nerdfont, mono/multi-select).
- Library fixed by CONTEXT: **ink** for all TUI rendering.
- The theme deliberately stays index-only (ADR-0005) — this is what lets taskthing inherit the user's
  terminal palette; every renderer must go through the theme resolver and never hardcode a color.
- **Multi-select is speculative** (no shipping command uses it) but kept in the primitive's design per
  CONTEXT — flagged so a reviewer knows it's intentional dead-weight, not an oversight.
- Dependency direction: TUI → porcelain (0003) for data/values, and → theme/glyph modules. Nothing below
  the porcelain layer depends on the TUI.
- ADR cross-reference: ANSI-16 role→index theming, no RGB, no named presets, `inbox-accent` as a role —
  ADR-0005.
