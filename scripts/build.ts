#!/usr/bin/env bun
// Compiles the CLI into a native binary for the machine running this script.
// Asset name mirrors the release workflow (.github/workflows/release.yml) and
// installers (scripts/install.sh, scripts/install.ps1): taskthing-<os>-<arch>,
// the same tokens updater.ts's selectAsset matches on.

const osTags: Record<string, string> = { linux: "linux", darwin: "darwin", win32: "win32" };
const archTags: Record<string, string> = { x64: "x64", arm64: "arm64" };

const osTag = osTags[process.platform];
const archTag = archTags[process.arch];
if (osTag === undefined || archTag === undefined) {
  console.error(`taskthing: unsupported build platform ${process.platform}/${process.arch}`);
  process.exit(1);
}

// Bun's --target spells Windows as "windows", though the shipped asset (and
// process.platform) says "win32".
const bunTarget = `bun-${osTag === "win32" ? "windows" : osTag}-${archTag}`;
const outfile = `dist/taskthing-${osTag}-${archTag}${osTag === "win32" ? ".exe" : ""}`;

console.log(`taskthing: building ${outfile} (target ${bunTarget})`);
await Bun.$`bun build src/cli.ts --compile --target=${bunTarget} --outfile ${outfile}`;
console.log(`taskthing: built ${outfile}`);
