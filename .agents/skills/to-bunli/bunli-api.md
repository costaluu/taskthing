# Bunli API surface

Condensed reference for porting a CLI to Bunli. Everything a handler needs is validated and typed; there are no raw, unschematized options.

## Project shape

```
my-cli/
├── src/
│   ├── index.ts          # entry: createCLI() + cli.run()
│   └── commands/         # one file per top-level command
│       └── greet.ts
├── .bunli/commands.gen.ts # generated types (auto-created; add to tsconfig)
├── bunli.config.ts
├── package.json          # bin → entry; scripts: dev/build/test
└── tsconfig.json
```

`package.json`:

```json
{
  "bin": { "my-cli": "./src/index.ts" },
  "scripts": { "dev": "bunli dev", "build": "bunli build", "test": "bun test" }
}
```

`bunli.config.ts` (auto-loaded by `createCLI()` — do not pass it manually):

```ts
import { defineConfig } from "@bunli/core";

export default defineConfig({
  name: "my-cli",
  version: "1.0.0",
  description: "My CLI tool",
  commands: { entry: "./src/index.ts", directory: "./src/commands" },
  plugins: [],
});
```

Entry — register explicitly with `cli.command(...)`:

```ts
import { createCLI } from "@bunli/core";
import greet from "./commands/greet";

const cli = await createCLI(); // loads bunli.config.ts automatically
cli.command(greet);
await cli.run();
```

Toolchain: `bunli dev [-- args]` (hot reload + codegen), `bunli build [--targets all]`, `bunli test`, `bunli release`. Type generation is always on; it regenerates on `dev` and `build`.

## Commands

```ts
import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

export default defineCommand({
  name: "greet" as const, // as const is REQUIRED for handler type inference
  description: "Greet someone",
  alias: ["g"], // string or string[]
  options: {
    name: option(z.string().min(1), { description: "Name to greet", short: "n" }),
    times: option(z.coerce.number().int().positive().default(1), { description: "Repeat", short: "t" }),
    loud: option(z.coerce.boolean().default(false), { description: "Shout", short: "l" }),
  },
  handler: async ({ flags, positional }) => {
    // flags.name: string, flags.times: number, flags.loud: boolean — all inferred, no annotation
  },
});
```

Rules:
- `name` must be `... as const`. A manual `{ flags }: {...}` annotation is the smell that `as const` is missing.
- Every option needs a schema. Use `z.coerce.*` for CLI input (all argv values arrive as strings).
- Defaults via `.default()`; optional via `.optional()`; omit both to make it required.

### Nested commands (groups)

```ts
export default defineCommand({
  name: "db" as const,
  alias: "d",
  description: "Database operations",
  commands: [
    defineCommand({ name: "migrate" as const, alias: "m", options: { /* ... */ }, handler: async () => {} }),
    defineCommand({ name: "seed" as const, handler: async () => {} }),
  ],
});
```

Resolution is **deepest-match**: `my-cli db migrate` matches the `db` group's `migrate` subcommand, never a top-level `db-migrate`. Use `--` to pass args through to a wrapped command: `my-cli git push -- --force`.

## Handler context

Destructure only what the command uses:

| Field | Use |
|---|---|
| `flags` | parsed + validated options |
| `positional` | non-flag args (array) |
| `shell` | Bun Shell (`Bun.$`) — replaces `execa`/`child_process`; auto-escapes |
| `env` | `process.env` |
| `cwd` | current working directory |
| `prompt` | interactive input (see below) |
| `spinner` | progress indicator |
| `colors` | terminal colors |
| `output` | write formatted structured output (respects `--format` / `outputPolicy`) |
| `context` | plugin store (present only when plugins are loaded) |
| `signal` | AbortSignal for cancellation |

```ts
handler: async ({ shell, flags, prompt, spinner }) => {
  const files = await shell`ls -la ${flags.dir}`.text();

  const proceed = await prompt.confirm("Continue?", { default: true });
  const name = await prompt("Your name?");
  const choice = await prompt.select("Color?", { options: ["red", "green"] });
  const secret = await prompt.password("Password:");

  const spin = spinner("Installing…"); spin.start();
  try { await shell`bun install`; spin.succeed("Done"); }
  catch (e) { spin.fail("Failed"); throw e; }
}
```

`outputPolicy: 'all' | 'agent-only'` on a command controls whether `output(...)` shows in a TTY or only to piped/agent consumers.

## Validation (Standard Schema)

Any Standard-Schema library works — Zod (default), Valibot, TypeBox, Arktype. Coercion maps hand-rolled parsing directly:

| Hand-rolled | Schema |
|---|---|
| `parseInt(v)` | `z.coerce.number()` |
| `v === "true"` | `z.coerce.boolean()` |
| `new Date(v)` | `z.coerce.date()` |
| `v.split(",")` | `z.string().transform(v => v.split(","))` |
| `--tag a --tag b` | `option(z.array(z.string()), { repeatable: true })` |

Constraints and unions carry over: `z.string().email()`, `z.enum(["dev","prod"])`, `z.coerce.number().int().min(1).max(65535)`, `z.union([...])`. Validation errors are caught and formatted automatically — don't hand-roll error printing for bad input.

## Plugins (optional, for cross-cutting behaviour)

Only reach for these if the original CLI had global middleware (timing, analytics, config-file merging, env detection). Skip otherwise.

```ts
import { createPlugin } from "@bunli/core/plugin";

const timing = createPlugin<{ start: number | null }>({
  name: "timing",
  store: { start: null },
  beforeCommand(ctx) { ctx.store.start = Date.now(); },
  afterCommand(ctx) { /* read ctx.store.start */ },
});

const cli = await createCLI({ plugins: [timing] as const }); // as const for store inference
```

Lifecycle order: `setup → configResolved → beforeCommand → preRun → [handler] → postRun → afterCommand`. Throwing in `setup`/`beforeCommand`/`preRun` halts; throwing in the others only logs. Store types from all plugins merge and surface on `context.store` in handlers.

Built-ins: `@bunli/plugin-config` (merge config from multiple sources), `@bunli/plugin-ai-detect` (detect AI-agent env).

## Codegen & typed execution

`.bunli/commands.gen.ts` is generated automatically (must be in `tsconfig.json`). It enables typed programmatic execution and discovery:

```ts
await cli.execute("deploy", ["--env", "production"]);        // args form
await cli.execute("deploy", { env: "production", dryRun: true }); // typed options form

import { cli as store } from "./.bunli/commands.gen";
store.list(); store.findByName("deploy"); store.getCommandNames();
```
