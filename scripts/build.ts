#!/usr/bin/env bun
// Compiles the CLI into native binaries through `bunli build`, then lays the
// artifacts out under the names the rest of the toolchain matches on.
//
// `bunli build` writes `dist/<target>/<entry-basename>` (and `.exe` on Windows),
// which cannot be uploaded as release assets — five files would all be called
// `index`. So the artifacts are renamed to `taskthing-<os>-<arch>`, the naming
// the release workflow (.github/workflows/release.yml), the installers
// (scripts/install.sh, scripts/install.ps1) and updater.ts's selectAsset share.
// Note bunli spells Windows `windows` while `process.platform` says `win32`;
// the rename is where those two meet.

import { rename, rm } from "node:fs/promises";
import { join } from "node:path";

/** bunli's compile target → the `process.platform`-`process.arch` asset token. */
const TARGETS: Record<string, string> = {
  "linux-x64": "linux-x64",
  "linux-arm64": "linux-arm64",
  "darwin-x64": "darwin-x64",
  "darwin-arm64": "darwin-arm64",
  "windows-x64": "win32-x64",
};

const requested = process.argv[2];
const targets =
  requested === undefined || requested === "native"
    ? [`${process.platform === "win32" ? "windows" : process.platform}-${process.arch}`]
    : requested === "all"
      ? Object.keys(TARGETS)
      : requested.split(",");

for (const target of targets) {
  if (TARGETS[target] === undefined) {
    console.error(`taskthing: unsupported build target ${target}`);
    process.exit(1);
  }
}

console.log(`taskthing: building ${targets.join(", ")}`);
// One invocation for every target: `bunli build` clears its outdir on each run,
// so building them one at a time would leave only the last.
await Bun.$`bunx bunli build --entry ./src/index.ts --targets ${targets.join(",")}`;

// A single target compiles straight into `dist/`; several nest one directory
// deep, one per target.
for (const target of targets) {
  const nested = targets.length > 1 ? join("dist", target) : "dist";
  const windows = target.includes("windows");
  const built = join(nested, windows ? "index.exe" : "index");
  const asset = join("dist", `taskthing-${TARGETS[target]}${windows ? ".exe" : ""}`);

  await rename(built, asset);
  if (targets.length > 1) await rm(nested, { recursive: true, force: true });
  console.log(`taskthing: built ${asset}`);
}
