#!/usr/bin/env bun
import { createCLI } from "@bunli/core";

import { VERSION } from "./build";

import add from "./commands/add";
import board from "./commands/board";
import boards from "./commands/boards";
import check from "./commands/check";
import clear from "./commands/clear";
import config from "./commands/config";
import deleteTask from "./commands/delete";
import entity from "./commands/entity";
import install from "./commands/install";
import list from "./commands/list";
import migrations from "./commands/migrations";
import pull from "./commands/pull";
import rebuild from "./commands/rebuild";
import recover from "./commands/recover";
import set from "./commands/set";
import star from "./commands/star";
import sync from "./commands/sync";
import theme from "./commands/theme";
import truncate from "./commands/truncate";
import uncheck from "./commands/uncheck";
import unstar from "./commands/unstar";
import update from "./commands/update";
import use from "./commands/use";
import workspace from "./commands/workspace";
import workspaces from "./commands/workspaces";

// Commands are registered explicitly rather than discovered from the commands
// directory: discovery resolves `.bunli/commands.gen.ts` against the *current
// working directory*, which the compiled binary — the only way taskthing ships —
// has no control over. Explicit imports are bundled into the binary and always
// resolve. Codegen still reads the directory, so the generated types stay whole.
// The name/version/description are passed inline rather than left to
// bunli.config.ts: `createCLI()` resolves that file against the current working
// directory, which the compiled binary — the only way taskthing ships — has no
// control over. bunli.config.ts still drives the toolchain (codegen, build).
const cli = await createCLI({
  name: "taskthing",
  version: VERSION,
  description: "a git-based task manager",
});

// Tasks, the default entity: its verbs sit at the top level because they are
// what a task manager is mostly asked to do.
cli.command(add);
cli.command(list);
cli.command(check);
cli.command(uncheck);
cli.command(star);
cli.command(unstar);
cli.command(deleteTask);
cli.command(recover);
cli.command(clear);
cli.command(set);

// The other entities carry their own verb tree.
cli.command(board);
cli.command(boards);
cli.command(workspace);
cli.command(workspaces);
cli.command(use);

// Sync: the git-facing verbs.
cli.command(sync);
cli.command(pull);
cli.command(rebuild);
cli.command(truncate);

// Config, themes, updates, setup.
cli.command(config);
cli.command(theme);
cli.command(update);
cli.command(migrations);
cli.command(install);

// Plumber.
cli.command(entity);

// `help` is a verb users already type (and `help <command>` reads the way git
// does); Bunli spells the same thing as a global flag. Translating here is the
// whole of it — routing still belongs to Bunli.
const argv = process.argv.slice(2);
await cli.run(argv[0] === "help" ? ["--help", ...argv.slice(1)] : argv);
