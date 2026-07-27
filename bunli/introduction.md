Introduction
The complete CLI development ecosystem for Bun

What is Bunli?
Bunli is a complete ecosystem for building command-line interfaces with Bun. It combines a minimal, type-safe framework with powerful tooling for development, testing, building, and distribution.

Type-Safe
Full TypeScript support with automatic type inference and code generation

// Types flow automatically
handler: async ({ flags }) => {
// flags is fully typed
}
// Generated types for advanced patterns
import { getCommandApi } from './commands.gen'
Zero Dependencies
Pure implementation with no external dependencies

"dependencies": {}
Fast Startup
Leverages Bun's quick boot time for instant CLI response

$ time mycli --help
0.012s
Production Ready
Automated builds, releases, and cross-platform distribution

bunli build --targets all
bunli release
Quick Start

# Create a new CLI project

bunx create-bunli my-cli
cd my-cli

# Start development

bunli dev

# Build for production

bunli build
Why Bunli?
Designed for Bun
Bunli is built specifically for the Bun runtime, taking advantage of:

Native TypeScript execution
Built-in test runner
Fast startup times
Bun Shell integration
Single-file executables
Type Inference First
Strong typing without manual annotations:

import { defineCommand, option } from "@bunli/core";
import { z } from "zod";
// Bunli infers everything
export default defineCommand({
name: "deploy",
options: {
env: option(z.enum(["dev", "staging", "prod"]), { description: "Target environment" }),
},
handler: async ({ flags }) => {
// TypeScript knows flags.env is 'dev' | 'staging' | 'prod'
switch (flags.env) {
case "dev": // ✅ Autocompleted
case "staging": // ✅ Autocompleted
case "prod": // ✅ Autocompleted
}
},
});
Standard Schema Support
Use any validation library you prefer:

import { defineCommand, option } from "@bunli/core";
import { z } from "zod";
export default defineCommand({
name: "serve",
description: "Example command",
options: {
email: option(z.string().email(), { description: "Email address" }),
port: option(z.coerce.number().int().min(1).max(65535), { description: "Port" }),
},
handler: async ({ flags }) => {
// flags.email is string
// flags.port is number
},
});
Complete Ecosystem
Everything you need to build production CLIs:

create-bunli - Project scaffolding
bunli-starter-template - GitHub template for real binary releases
bunli dev - Hot reload development with type generation
bunli build - Multi-platform builds
bunli generate - Generate TypeScript definitions from commands
bunli test - Testing utilities
bunli release - Automated versioning and publishing (npm + tags)
Next Steps
Getting Started
Create your first Bunli CLI

Core Concepts
Learn the fundamentals

Examples
See Bunli in action

API Reference
Explore the full API
