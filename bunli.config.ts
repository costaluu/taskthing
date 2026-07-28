import { defineConfig } from "@bunli/core";

import { VERSION } from "./src/build";

export default defineConfig({
  name: "taskthing",
  version: VERSION,
  description: "a git-based task manager",
  commands: {
    entry: "./src/index.ts",
    directory: "./src/commands",
  },
});
