#!/usr/bin/env bun
import { buildCli } from "./app";
import { inkPresenter } from "./presenter-ink";
import { plainPresenter } from "./presenter-plain";
import { setPresenter } from "./presenter";

// The entry: pick how output is drawn, then hand argv to the command tree.

// The one place the styled/piped fork is decided: every command draws through
// whichever adapter is set here, and none of them asks again.
setPresenter(process.stdout.isTTY ? inkPresenter() : plainPresenter());

const cli = await buildCli();

// `help` is a verb users already type (and `help <command>` reads the way git
// does); Bunli spells the same thing as a global flag. Translating here is the
// whole of it — routing still belongs to Bunli.
const argv = process.argv.slice(2);
await cli.run(argv[0] === "help" ? ["--help", ...argv.slice(1)] : argv);
