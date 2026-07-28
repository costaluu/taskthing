import { option } from "@bunli/core";
import { z } from "zod";

/**
 * `--workspace=<name>`: the workspace a command acts on. Nearly every command
 * takes it, and nearly every invocation omits it — the current workspace from
 * config.md is the default.
 */
export const workspaceOption = () =>
  option(z.string().optional(), {
    description: "the workspace to act on (default: the current one)",
  });
