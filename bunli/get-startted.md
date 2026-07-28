Getting Started
Create your first CLI with Bunli in under 5 minutes

Get up and running with Bunli in just a few minutes. This guide will walk you through creating your first CLI application.

Prerequisites
Bunli requires Bun 1.0 or later. Install it from bun.sh

# Check your Bun version

bun --version
Quick Start
Create a new project
Use create-bunli to scaffold a new CLI project:

bunx create-bunli my-cli
cd my-cli
You'll be prompted for:

Project name: Validated to contain only lowercase letters, numbers, and hyphens
Template: Choose from basic (default), advanced, or monorepo
Package manager: Select your preferred package manager (bun, npm, yarn, pnpm)
The tool will automatically:

✅ Create the project structure
✅ Initialize a git repository
✅ Install dependencies
✅ Set up TypeScript and testing
Explore the project structure
Your new CLI project includes:

my-cli/
├── src/
│ ├── index.ts # CLI entry point
│ └── commands/ # Command definitions
│ └── hello.ts # Individual command files
├── .bunli/ # Generated files (auto-created)
│ └── commands.gen.ts # Generated TypeScript types
├── bunli.config.ts # Configuration
├── package.json
├── tsconfig.json
└── README.md
Run your CLI
In development mode with hot reload:

bunli dev
This starts your CLI with automatic hot reload enabled. Your CLI name will be used directly (e.g., if name: 'my-cli' in config, run my-cli).

To pass arguments to your CLI:

bunli dev -- --verbose
Add your first command
Create a new command in src/commands/greet.ts:

import { defineCommand, option } from "@bunli/core";
import { z } from "zod";
export default defineCommand({
name: "greet" as const,
description: "Greet someone",
options: {
name: option(z.string().min(1), { description: "Name to greet", short: "n" }),
excited: option(z.coerce.boolean().default(false), {
description: "Add excitement",
short: "e",
}),
},
handler: async ({ flags }) => {
const greeting = `Hello, ${flags.name}${flags.excited ? "!" : "."}`;
console.log(greeting);
},
});
Test your command

# Run in development

bunli dev greet --name World --excited

# Output: Hello, World!

# Run tests

bunli test
Build for production
Create optimized binaries for distribution:

# Build for current platform

bunli build

# Build for all platforms

bunli build --targets all
Project Templates
Create-bunli offers several templates to match your needs:

Simple single-command CLI: bash bunx create-bunli my-cli --template basic Perfect for: - Learning Bunli - Simple scripts - Single-purpose tools Includes: - TypeScript setup

Example command with tests - Build configuration
External Templates
Use any GitHub repository as a template:

# GitHub repository

bunx create-bunli my-cli --template username/repo

# Specific branch

bunx create-bunli my-cli --template username/repo#branch

# NPM package

bunx create-bunli my-cli --template npm:template-package
Manual Setup
If you prefer to set up manually:

Install Bunli

bun add @bunli/core
bun add -d bunli
Create your CLI entry point

// src/cli.ts
import { createCLI } from "@bunli/core";
const cli = await createCLI({
name: "my-cli",
version: "1.0.0",
description: "My awesome CLI",
});
cli.command({
name: "hello" as const,
description: "Say hello",
handler: async () => {
console.log("Hello from Bunli!");
},
});
await cli.run();
Add to package.json

{
"name": "my-cli",
"bin": {
"my-cli": "./src/cli.ts"
},
"scripts": {
"dev": "bunli dev",
"build": "bunli build",
"test": "bun test"
}
}
Create bunli.config.ts

import { defineConfig } from "@bunli/core";
export default defineConfig({
name: "my-cli",
version: "1.0.0",
commands: {
directory: "./src/commands",
},
});
What's Next?
Now that you have a working CLI:

Learn about Commands - The building blocks of your CLI
Explore Type Inference - Leverage TypeScript's power
Add Validation - Ensure correct input
Generate Type Definitions - Enhanced developer experience
Write Tests - Keep your CLI reliable
Build & Distribute - Share your CLI with the world
