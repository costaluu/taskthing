import type { Transform } from "./mdwal";
import type { ConfigMigration, Migration } from "./migration-runner";

// ── build info ───────────────────────────────────────────────────────────────
//
// What the compiled binary knows about itself: its own semver, and the set of
// structural migrations bound to the versions up to it. The binary is the sole
// source of truth for which migrations exist (Spec 0005 story 21); a release
// bundles the new binary and its migration(s) together. The set is empty until
// the first structural migration ships.

/** This binary's version. Real builds stamp it from the release tag. */
export const VERSION = "0.1.0";

/** The embedded structural migration set, version-bound and ordered. */
export const MIGRATIONS: Migration[] = [];

/**
 * The embedded config-migration set: version-keyed rewrites of the global
 * config.md, applied once per machine during an update. Empty until the first
 * config-schema change ships; a migration author adds an entry here that brings
 * an old value forward (mdwal never coerces one on its own — Spec 0005).
 */
export const CONFIG_MIGRATIONS: ConfigMigration[] = [];

/**
 * The embedded data-migration transforms (ADR-0007): the migrate-on-read chain
 * that brings an entity event authored under an older schema up to the current
 * one, applied by mdwal `replay` per event (never logged, never one-shot). This
 * is the only place a *data* migration lives — folder/`config.md` changes go
 * through the migration runner instead.
 *
 * Empty until a schema change that cannot be read as-is ships (a non-coercible
 * `field change-type`, or a `field rename` — the log carries the old key/value
 * forever, so a derived one-shot rewrite would be reverted by the next replay).
 * To author one: bump {@link VERSION}, edit the zod schema, and append a
 * `{ version, up }` whose `version` is the new VERSION and whose `up` maps an
 * older event's payload onto the new schema. Each `up` must be **pure and
 * deterministic** — every binary of that version has to produce the identical
 * result, or peers would not converge.
 */
export const TRANSFORMS: Transform[] = [];
