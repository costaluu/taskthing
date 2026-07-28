Configuration
Configure your Bunli CLI project with bunli.config.ts

Configure your Bunli CLI project behavior with bunli.config.ts. Configuration is automatically loaded - you don't need to manually import or pass config to createCLI().

Automatic Configuration Loading
Bunli automatically loads configuration from bunli.config.ts when you call createCLI():

// cli.ts - No config needed!
import { createCLI } from "@bunli/core";
import build from "./commands/build.js";
import test from "./commands/test.js";
const cli = await createCLI(); // Automatically loads bunli.config.ts
cli.command(build);
cli.command(test);
await cli.run();

// bunli.config.ts - All configuration here
import { defineConfig } from "@bunli/core";
export default defineConfig({
name: "my-cli",
version: "1.0.0",
description: "My awesome CLI tool",
commands: {
entry: "./cli.ts",
directory: "./commands", // optional fallback hint
},
plugins: [],
});
Benefits of automatic loading: - Single source of truth - Configuration lives in one place

No duplication - Don't repeat name, version, description in cli.ts - Consistent - Same config used by bunli CLI commands and runtime - Type-safe - Full TypeScript support with defineConfig
Configuration Override
You can override specific settings for runtime-specific needs:

// cli.ts - Override plugins with runtime options
import { createCLI } from "@bunli/core";
import { configMergerPlugin } from "@bunli/plugin-config";
const cli = await createCLI({
plugins: [configMergerPlugin({ sources: [".myrc.json"] })] as const,
});
await cli.run();
The override merges with the loaded config (override values win).

Configuration File
Create a bunli.config.ts file in your project root:

import { defineConfig } from "@bunli/core";
export default defineConfig({
name: "my-cli",
version: "1.0.0",
description: "My awesome CLI tool",
// Command discovery options
commands: {
entry: "./cli.ts",
directory: "./commands", // optional
},
// Build configuration
build: {
entry: "./cli.ts",
outdir: "./dist",
targets: ["native"], // Example target
compress: false, // Default: false
minify: false, // Default: false
sourcemap: true, // Default: true
},
// Plugins (optional, defaults to [])
plugins: [],
});
Configuration Options
Basic Information

export default defineConfig({
// CLI name (defaults to package.json name)
name: "my-cli",
// Version (defaults to package.json version)
version: "1.0.0",
// Description for help text
description: "A powerful CLI tool",
});
Command Configuration

export default defineConfig({
commands: {
// CLI entry used for command discovery/codegen
entry: "./cli.ts",
// Optional fallback directory scanner
directory: "./commands",
},
});
commands.manifest has been removed. Register commands explicitly with cli.command(...).

Build Configuration

export default defineConfig({
build: {
// Entry point (default: auto-detected)
entry: "./cli.ts",
// Output directory (default: './dist')
outdir: "./dist",
// Target platforms (default: [])
targets: ["native"], // Native for current platform
// or specific platforms:
// targets: ['darwin-arm64', 'linux-x64', 'windows-x64'],
// Compress output archives (default: false)
compress: false,
// External dependencies (not bundled)
external: ["sqlite3", "sharp"],
// Minify output (default: false)
minify: false,
// Generate sourcemaps (default: true)
sourcemap: true,
},
});
Development Configuration

export default defineConfig({
dev: {
// Watch for file changes
watch: true,
// Enable Node.js inspector
inspect: true,
// Inspector port
port: 9229,
},
});
TUI Configuration
Configure terminal UI rendering behavior:

export default defineConfig({
tui: {
renderer: {
// Use alternate screen buffer (default: 'standard')
bufferMode: "alternate", // or 'standard'
},
image: {
// Image protocol mode (default: 'auto')
mode: "auto", // 'off' | 'auto' | 'on'
// Image protocol (default: 'auto')
protocol: "kitty", // 'auto' | 'kitty'
// Default image width
width: 800,
// Default image height
height: 600,
},
},
});
Help Configuration

export default defineConfig({
help: {
renderer: myCustomRenderer, // Custom HelpRenderer function (any type accepted)
},
});
Type Generation
Type generation is automatically enabled in all Bunli projects for enhanced developer experience:

export default defineConfig({
name: "my-cli",
version: "1.0.0",
});
Default Behavior:

Setting Default Description
Auto-Generation true Always enabled
Command Entry build.entry or auto-detected CLI entry used for command discovery
Output File ./.bunli/commands.gen.ts Generated types location
Watch Mode true in dev Regenerate on file changes
Plugins [] Empty array by default
Type generation is automatically integrated with bunli dev and bunli build commands. Learn more in the Type Generation Guide.

Test Configuration

export default defineConfig({
test: {
// Test file patterns
pattern: ["**/*.test.ts", "**/*.spec.ts"],
// Coverage reporting
coverage: true,
// Watch mode
watch: false,
},
});
Release Configuration

export default defineConfig({
release: {
// Publish to npm
npm: true,
// Create GitHub release
github: true,
// Git tag format
tagFormat: "v{{version}}",
// Use conventional commits
conventionalCommits: true,
// Optional: npm binary package mode
binary: {
packageNameFormat: "{{name}}-{{platform}}",
shimPath: "bin/run.mjs",
},
},
});
Workspace Configuration
For monorepos:

export default defineConfig({
workspace: {
// Package locations
packages: ["packages/*", "apps/*"],
// Shared configuration
shared: {
typescript: true,
eslint: true,
},
// Version strategy
versionStrategy: "independent", // or 'fixed'
},
});
Environment-Specific Config
Use environment variables or conditions:

export default defineConfig({
name: "my-cli",
build: {
minify: process.env.NODE_ENV === "production",
sourcemap: process.env.DEBUG === "true",
targets: process.env.CI
? ["darwin-arm64", "linux-x64", "windows-x64"]
: [process.platform + "-" + process.arch],
},
});
Extending Configuration
Share configuration across projects:

// base.config.ts
export const baseConfig = {
build: {
minify: true,
compress: true,
},
};
// bunli.config.ts
import { defineConfig } from "@bunli/core";
import { baseConfig } from "./base.config.js";
export default defineConfig({
...baseConfig,
name: "my-cli",
build: {
...baseConfig.build,
entry: "./src/index.ts",
},
});
Platform-Specific Builds
Configure different settings per platform:

const platforms = {
"darwin-arm64": {
external: ["fsevents"],
},
"windows-x64": {
external: ["node-gyp"],
},
};
export default defineConfig({
build: {
targets: Object.keys(platforms),
// Platform-specific externals handled by build process
},
});
Configuration Schema
The configuration is validated using Zod:

const configSchema = z.object({
name: z.string().optional(),
version: z.string().optional(),
description: z.string().optional(),
commands: z
.object({
entry: z.string().optional(),
directory: z.string().optional(),
generateReport: z.boolean().optional(),
})
.optional(),
build: z
.object({
entry: z.string().or(z.array(z.string())).optional(),
outdir: z.string().optional(),
targets: z.array(z.string()).optional(),
compress: z.boolean().optional(),
external: z.array(z.string()).optional(),
minify: z.boolean().optional(),
sourcemap: z.boolean().optional(),
})
.optional(),
dev: z
.object({
watch: z.boolean().optional(),
inspect: z.boolean().optional(),
port: z.number().optional(),
})
.optional(),
test: z
.object({
pattern: z.string().or(z.array(z.string())).optional(),
coverage: z.boolean().optional(),
watch: z.boolean().optional(),
})
.optional(),
workspace: z
.object({
packages: z.array(z.string()).optional(),
shared: z.unknown().optional(),
versionStrategy: z.enum(["fixed", "independent"]).optional(),
})
.optional(),
release: z
.object({
npm: z.boolean().optional(),
github: z.boolean().optional(),
tagFormat: z.string().optional(),
conventionalCommits: z.boolean().optional(),
binary: z
.object({
packageNameFormat: z.string().optional(),
shimPath: z.string().optional(),
})
.optional(),
})
.optional(),
plugins: z.array(z.unknown()).optional(),
tui: z
.object({
renderer: z.object({ bufferMode: z.enum(["alternate", "standard"]).optional() }).optional(),
image: z
.object({
mode: z.enum(["off", "auto", "on"]).optional(),
protocol: z.enum(["auto", "kitty"]).optional(),
width: z.number().optional(),
height: z.number().optional(),
})
.optional(),
})
.optional(),
});
Loading Configuration
Bunli automatically looks for configuration files in this order:

bunli.config.ts
bunli.config.js
bunli.config.mjs
The configuration is loaded and validated before any CLI commands run.

Default Values
If no configuration file is found, these defaults are used:

{
name: package.name,
version: package.version,
description: package.description,
commands: {
entry: './cli.ts'
},
build: {
entry: './cli.ts',
outdir: './dist',
targets: [],
compress: false,
minify: false,
sourcemap: true
},
plugins: []
}
Best Practices
Keep It Simple: Start with minimal configuration
Use TypeScript: Get autocomplete and type checking
Environment Variables: Use for deployment-specific settings
Share Common Config: Extract shared settings to separate files
Document Options: Add comments for team members
