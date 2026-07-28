import type { Command } from "@bunli/core";

/**
 * The subcommands of a command that also has a handler of its own.
 *
 * Two kinds of command need this. Some run bare *and* group — `config` (bare:
 * open settings; plus `config set …`), `update` (bare: check; plus `update
 * apply`). The rest are pure groups that must still **fail** on a subcommand
 * they do not have: a handler-less Bunli group answers `taskthing set size 1 x`
 * by printing its help and exiting 0, which would let a typo pass for success in
 * a script. Giving the group a handler puts the unknown-subcommand error back
 * (see {@link rejectUnknownSubcommand}).
 *
 * Bunli's router supports the shape either way: it runs the handler when nothing
 * deeper matches, and deepest-matches a subcommand when one does. Its *types* do
 * not — a runnable command declares `commands?: undefined` and a `Group`
 * declares `handler?: undefined`, so the two are mutually exclusive at the type
 * level only. Writing them through this helper keeps the module a plain
 * `defineCommand({…})` call, which is also what the codegen parser expects to
 * find as a command file's default export.
 */
export function subcommands(commands: Command<any, {}, any>[]): undefined {
  return commands as unknown as undefined;
}

/**
 * The handler a pure group carries so an unknown subcommand is reported rather
 * than answered with help and a zero exit. It only ever runs when the router
 * found no deeper match, so reaching it *is* the mistake.
 */
export function rejectUnknownSubcommand(message: (typed: string) => string) {
  return async ({ positional }: { positional: string[] }): Promise<never> => {
    throw new Error(message(positional[0] ?? ""));
  };
}
