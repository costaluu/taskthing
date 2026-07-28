// Only what Bun has no equivalent for: directory and rename operations, plus
// appending to a file (Bun.write always replaces). Reads and whole-file writes
// go through Bun.file / Bun.write.
import { appendFile, mkdir, readdir, rename, rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import type { ZodType } from "zod";

// ── Public contract ─────────────────────────────────────────────────────────
//
// mdwal is bound to a set of zod schemas (one per entity type) and a workspace
// root directory. It performs every structural/data operation on the workspace
// and, in a *remote* workspace, records each operation as one append-only line
// in events.log. The zod schema is the only source of truth for field types.

export type WorkspaceMode = "local" | "remote";

export interface MdwalConfig {
  /** Absolute path to the workspace root. */
  root: string;
  /** Entity type → zod schema (the only source of truth for field types). */
  schemas: Record<string, ZodType>;
  /**
   * local = no events.log; remote = one log line per logged operation. A
   * workspace can stop being remote (disassociation) without the engine being
   * rebuilt, so this may instead be a function answering the question live.
   *
   * That function must be async even where the answer is at hand: deciding it
   * means asking the world (does the repository still have a remote?), and a
   * sync signature would silently accept a predicate whose result is a promise —
   * always truthy, so the workspace would look remote forever.
   */
  mode: WorkspaceMode | (() => Promise<WorkspaceMode>);
  /** Injectable monotonic clock yielding nanosecond timestamps. */
  clock: () => bigint;
  /** Injectable author source (global git user.name in production). */
  author: () => string;
  /** Injectable entity-id source; defaults to the 21-char nanoid generator. */
  ids?: () => string;
  /** This binary's semver; stamped on every event and the migrate-on-read target. */
  version?: string;
  /** Ordered data-migration transforms, keyed by the version they shipped at (ADR-0007). */
  transforms?: Transform[];
}

/**
 * A data-migration transform (ADR-0007): `up` rewrites an event authored under
 * an older schema so it looks as if authored under the schema that shipped at
 * `version`. Must be pure and deterministic across every binary of that version.
 */
export interface Transform {
  version: string;
  up: (ev: Event) => Event;
}

/** A materialized entity: its stable id plus the schema-validated fields. */
export type ParsedEntity = { id: string } & Record<string, unknown>;

/** The op set is closed; no op ever combines multiple actions. */
export type Op =
  | "CREATE"
  | "UPDATE_FIELD"
  | "CREATE_FOLDER"
  | "RENAME_FOLDER"
  | "TRUNCATE_FOLDER"
  | "DELETE_FOLDER";

export interface Event {
  ts: bigint;
  op: Op;
  entityType: string;
  entityId: string;
  /**
   * All free-form content lives here (never in the header). Shapes by op:
   * - CREATE:        { author, snapshot }
   * - UPDATE_FIELD:  { author, field, value }
   * - CREATE_FOLDER: { author }
   * - RENAME_FOLDER: { author, to }   (header carries the previous name)
   * - TRUNCATE_FOLDER: { author }
   * - DELETE_FOLDER:   { author }
   */
  payload: Record<string, any>;
}

/** Derived, LWW-resolved fields for one entity (includes its LWW companions). */
export type EntityState = Record<string, unknown>;

/** A base/derived snapshot keyed by `${entityType}/${entityId}`. */
export type Snapshot = Record<string, EntityState>;

/**
 * Per-operation logging choice. Defaults to logging (in a remote workspace);
 * `{ log: false }` mutates the workspace without entering this peer's authored
 * history — the migration path, so a change every peer applies identically is
 * never mistaken for a user-authored, LWW-subject event.
 */
export interface LogOption {
  log?: boolean;
}

export interface Mdwal {
  createEntity(
    entityType: string,
    snapshot: Record<string, unknown>,
    opts?: LogOption,
  ): Promise<ParsedEntity>;
  /**
   * Fold events over a base snapshot under per-field LWW, without re-logging.
   * A field's value is taken from the event with the greatest timestamp; a
   * losing event is simply not applied. Deterministic and order-independent.
   */
  replay(base: Snapshot, events: Event[]): Snapshot;
  updateField(
    entityType: string,
    id: string,
    field: string,
    value: unknown,
    opts?: LogOption,
  ): Promise<void>;
  /**
   * Create the entity folder for `entityType`. Folder ops carry the folder name
   * in the header's entity-type position and leave the entity-id position empty.
   */
  /**
   * Record an already-stored entity as a retroactive `CREATE`, under the id it
   * already has and carrying its current snapshot. Used when a workspace with
   * prior local work joins a remote: the work enters the shared history instead
   * of staying invisible to other peers. Writes no files.
   */
  logExisting(entityType: string, id: string): Promise<void>;
  /**
   * Remove an entity's derived file from this machine, recording nothing. Local
   * tidying, not a change other peers should learn about — and since the
   * entity's events are untouched, a later replay rebuilds the file.
   */
  discard(entityType: string, id: string): Promise<void>;
  createFolder(entityType: string, opts?: LogOption): Promise<void>;
  /** Rename an entity folder, carrying its entities with it. */
  renameFolder(entityType: string, to: string, opts?: LogOption): Promise<void>;
  /** Clear an entity folder's entities as one recorded action; the folder stays. */
  truncateFolder(entityType: string, opts?: LogOption): Promise<void>;
  /** Remove an entity folder along with its entities. */
  deleteFolder(entityType: string, opts?: LogOption): Promise<void>;
  /**
   * The fixed entity folders, derived from the bound schemas — the set of
   * folders that hold derived state. Consumers (the manager's `.gitignore`)
   * read it from here so an entity type added by a migration updates it with no
   * second place to keep in sync.
   */
  folders(): string[];
  /** The entity types the bound schemas declare. */
  entityTypes(): string[];
  /** This binary's semver — what its events are stamped with (ADR-0007). */
  version(): string | undefined;
  /**
   * The whole workspace's derived state, in the shape `replay` folds over — so
   * what is on disk can serve as the base of a replay.
   */
  snapshot(): Promise<Snapshot>;
  /**
   * Write a replayed snapshot to the workspace's derived files. Re-deriving is
   * not authoring: nothing is ever logged, whatever the workspace's mode.
   */
  materialize(state: Snapshot): Promise<void>;
  read(entityType: string, id: string): Promise<ParsedEntity>;
  /**
   * Materialize every entity of a type. A type whose folder doesn't exist yet
   * materializes as no entities, not as an error.
   */
  readAll(entityType: string): Promise<ParsedEntity[]>;
  parseLog(): Promise<Event[]>;
  /**
   * Parse log text that came from somewhere other than this workspace — another
   * peer's log, read out of git during consolidation and never written to disk.
   */
  parseEvents(text: string): Event[];
  /**
   * Render events as log text. The inverse of `parseEvents`: reading a log and
   * writing it back leaves it byte-identical, so rewriting one (purge, or a
   * migration replay) can never perturb the events it keeps.
   */
  serializeLog(events: Event[]): string;
  /** Render an entity's fields as markdown-with-frontmatter. */
  serializeEntity(fields: Record<string, unknown>): string;
  /**
   * Read markdown-with-frontmatter back into bare fields, the inverse of
   * `serializeEntity`. Exposed because the same path serves files that live
   * outside any workspace and outside LWW — config.md above all.
   */
  parseEntity(text: string): Record<string, unknown>;
  /**
   * Drop this peer's own events with `ts <= threshold` from events.log. Safe by
   * construction: because the log is monotonic, every event appended after a
   * rebuild has `ts > threshold`, so an unconsolidated event is never purged.
   */
  purge(threshold: bigint): Promise<void>;
}

/**
 * Folder-name sentinel for the workspace root. Folder ops on the root carry it
 * in the header instead of the folder's current or previous name, so a chain of
 * renames replays unambiguously.
 */
export const ROOT_FOLDER = "$root";

/**
 * The virtual board every task belongs to when it has no real board (ADR-0004).
 * No entity may ever be minted with this id, or it would shadow the sentinel.
 */
export const INBOX = "inbox";

// ── nanoid (21-char, URL-safe alphabet) ─────────────────────────────────────
const NANOID_ALPHABET =
  "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict";

function nanoid(size = 21): string {
  const bytes = crypto.getRandomValues(new Uint8Array(size));
  let id = "";
  for (let i = 0; i < size; i++) id += NANOID_ALPHABET[bytes[i]! & 63];
  return id;
}

// ── frontmatter (markdown-with-frontmatter) ─────────────────────────────────
// Scalar values are stored JSON-encoded, which is a valid YAML subset and lets
// strings/numbers/booleans round-trip losslessly under the schema. LWW
// `_lastModified` companions are nanosecond bigints, which JSON can't represent,
// so they are written as a bare decimal token and read back with BigInt.
// Keys whose values are nanosecond bigints (not JSON-representable): the
// per-field `<field>_lastModified` companions and the derived `createdAt`.
function isTimestampKey(key: string): boolean {
  return key.endsWith("_lastModified") || key === "createdAt";
}

function serializeEntity(fields: Record<string, unknown>): string {
  const lines = Object.entries(fields).map(([k, v]) =>
    typeof v === "bigint" ? `${k}: ${v.toString()}` : `${k}: ${JSON.stringify(v)}`,
  );
  return `---\n${lines.join("\n")}\n---\n`;
}

function parseFrontmatter(text: string): Record<string, unknown> {
  const match = text.match(/^---\n([\s\S]*?)\n---/);
  const body = match?.[1] ?? "";
  const out: Record<string, unknown> = {};
  for (const line of body.split("\n")) {
    if (!line.trim()) continue;
    const idx = line.indexOf(": ");
    if (idx === -1) continue;
    const key = line.slice(0, idx);
    const token = line.slice(idx + 2);
    out[key] = isTimestampKey(key) ? BigInt(token) : JSON.parse(token);
  }
  return out;
}

// ── log line: <ts>::<op>::<entityType>::<entityId>::<payload_json> ───────────
function serializeEvent(ev: Event): string {
  const header = [ev.ts.toString(), ev.op, ev.entityType, ev.entityId];
  return `${header.join("::")}::${JSON.stringify(ev.payload)}`;
}

function serializeLog(events: Event[]): string {
  return events.length === 0 ? "" : events.map(serializeEvent).join("\n") + "\n";
}

function parseEvents(text: string): Event[] {
  return text
    .split("\n")
    .filter((line) => line.length > 0)
    .map(parseEvent);
}

function parseEvent(line: string): Event {
  let rest = line;
  const header: string[] = [];
  for (let k = 0; k < 4; k++) {
    const idx = rest.indexOf("::");
    header.push(rest.slice(0, idx));
    rest = rest.slice(idx + 2);
  }
  const [ts, op, entityType, entityId] = header as [string, Op, string, string];
  return { ts: BigInt(ts), op, entityType, entityId, payload: JSON.parse(rest) };
}

// Snapshot keys are `${entityType}/${entityId}`; an id never contains a slash
// (it is a nanoid), so the first slash is the separator.
function splitKey(key: string): [string, string] {
  const idx = key.indexOf("/");
  return [key.slice(0, idx), key.slice(idx + 1)];
}

// ── replay / LWW fold ────────────────────────────────────────────────────────
// Apply one field's value under LWW: it wins only if strictly newer than the
// field's current `_lastModified`. Ties (astronomically unlikely at ns) keep
// the incumbent, which is deterministic for a fixed comparison.
function applyField(
  entity: EntityState,
  field: string,
  value: unknown,
  ts: bigint,
  author: string,
): void {
  const current = entity[`${field}_lastModified`] as bigint | undefined;
  if (current !== undefined && ts <= current) return;
  entity[field] = value;
  entity[`${field}_lastModified`] = ts;
  entity[`${field}_lastModifiedBy`] = author;
}

// ── migrate-on-read (ADR-0007) ───────────────────────────────────────────────
/**
 * Compare two binary semvers. Version ordering is what decides which data
 * migrations apply and whether data is readable at all (ADR-0007), so the
 * comparison lives with the engine that owns those rules.
 */
export function compareSemver(a: string, b: string): number {
  const pa = a.split(".").map(Number);
  const pb = b.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d !== 0) return d;
  }
  return 0;
}

// Bring one event up to the current schema: apply, in ascending version order,
// every transform whose schema-change version is newer than the event's own.
function migrateEvent(
  ev: Event,
  currentVersion: string | undefined,
  transforms: Transform[] | undefined,
): Event {
  const v = ev.payload.version as string | undefined;
  if (v === undefined) return ev;
  // Version barrier: an event newer than this binary can't be transformed down.
  if (currentVersion !== undefined && compareSemver(v, currentVersion) > 0) {
    throw new Error(`event authored by a newer version ${v}; update taskthing to >= ${v}`);
  }
  if (!transforms) return ev;
  const chain = transforms
    .filter((t) => compareSemver(t.version, v) > 0)
    .sort((a, b) => compareSemver(a.version, b.version));
  let out = ev;
  for (const t of chain) out = t.up(out);
  return out;
}

function foldEvents(base: Snapshot, events: Event[]): Snapshot {
  const state: Snapshot = {};
  for (const [key, fields] of Object.entries(base)) state[key] = { ...fields };

  for (const ev of events) {
    const key = `${ev.entityType}/${ev.entityId}`;
    const entity = (state[key] ??= {});
    const author = ev.payload.author as string;
    if (ev.op === "CREATE") {
      const snapshot = ev.payload.snapshot as Record<string, unknown>;
      for (const [f, v] of Object.entries(snapshot)) {
        applyField(entity, f, v, ev.ts, author);
      }
    } else {
      applyField(entity, ev.payload.field, ev.payload.value, ev.ts, author);
    }
  }
  return state;
}

// ── engine ──────────────────────────────────────────────────────────────────
export function createMdwal(config: MdwalConfig): Mdwal {
  // The workspace root is itself renameable (via the `$root` sentinel), so every
  // path is derived from this mutable current root, never from config.root.
  let root = config.root;
  const logPathOf = () => join(root, "events.log");
  const folderNameOf = (entityType: string) => `${entityType}s`;
  const folderOf = (entityType: string) => join(root, folderNameOf(entityType));
  const fileOf = (entityType: string, id: string) =>
    join(folderOf(entityType), `${id}.md`);

  function schemaFor(entityType: string): ZodType {
    const schema = config.schemas[entityType];
    if (!schema) throw new Error(`no schema for entity type "${entityType}"`);
    return schema;
  }

  // An event is appended only in a remote workspace, and only when the caller
  // didn't opt out (migrations mutate the workspace with logging off).
  const shouldLog = async (opts?: LogOption) => {
    if ((opts?.log ?? true) === false) return false;
    const mode = typeof config.mode === "function" ? await config.mode() : config.mode;
    return mode === "remote";
  };

  // Mint an id, refusing the inbox sentinel (ADR-0004). The shipped 21-char
  // generator can't produce it, but the refusal makes the invariant enforced
  // rather than a property of the current id length.
  function newId(): string {
    const generate = config.ids ?? nanoid;
    let id = generate();
    while (id === INBOX) id = generate();
    return id;
  }

  // A workspace that has never logged anything simply has no log yet.
  async function readLog(): Promise<Event[]> {
    try {
      return parseEvents(await Bun.file(logPathOf()).text());
    } catch {
      return [];
    }
  }

  // The ids of every entity stored under a type; a type whose folder doesn't
  // exist yet simply has none.
  async function idsIn(entityType: string): Promise<string[]> {
    try {
      const files = await readdir(folderOf(entityType));
      return files
        .filter((f) => f.endsWith(".md"))
        .map((f) => f.slice(0, -".md".length));
    } catch {
      return [];
    }
  }

  async function readEntity(
    entityType: string,
    id: string,
  ): Promise<ParsedEntity> {
    const schema = schemaFor(entityType);
    const raw = parseFrontmatter(await Bun.file(fileOf(entityType, id)).text());
    const fields = schema.parse(raw) as Record<string, unknown>;

    // The schema strips its companions/unknowns; re-attach the derived LWW
    // metadata (already typed by parseFrontmatter: bigint ts, string author)
    // and derive createdAt (stored) + updatedAt (max field _lastModified).
    const companions: Record<string, unknown> = {};
    let updatedAt: bigint | undefined;
    for (const [k, v] of Object.entries(raw)) {
      if (k.endsWith("_lastModified")) {
        companions[k] = v;
        if (updatedAt === undefined || (v as bigint) > updatedAt) {
          updatedAt = v as bigint;
        }
      } else if (k.endsWith("_lastModifiedBy")) {
        companions[k] = v;
      }
    }
    return { id, ...fields, ...companions, createdAt: raw.createdAt, updatedAt };
  }

  return {
    async createEntity(entityType, snapshot, opts) {
      const schema = schemaFor(entityType);
      const fields = schema.parse(snapshot) as Record<string, unknown>;
      const id = newId();

      // Every mutating op consumes a monotonic timestamp regardless of mode —
      // the LWW metadata is part of the file format in local and remote alike;
      // only the events.log append is gated on the workspace being remote.
      const ts = config.clock();
      const author = config.author();

      // The file carries the fields, each field's LWW companions (all last
      // modified at the create ts), and the derived createdAt. The event's
      // snapshot stays schema-fields-only.
      const fileFields: Record<string, unknown> = { createdAt: ts };
      for (const [f, v] of Object.entries(fields)) {
        fileFields[f] = v;
        fileFields[`${f}_lastModified`] = ts;
        fileFields[`${f}_lastModifiedBy`] = author;
      }

      await mkdir(folderOf(entityType), { recursive: true });
      await Bun.write(fileOf(entityType, id), serializeEntity(fileFields));

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "CREATE",
          entityType,
          entityId: id,
          payload: { author, snapshot: fields, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }

      return { id, ...fields };
    },

    replay(base, events) {
      const migrated = events.map((ev) =>
        migrateEvent(ev, config.version, config.transforms),
      );
      return foldEvents(base, migrated);
    },

    async updateField(entityType, id, field, value, opts) {
      const ts = config.clock();
      const author = config.author();
      const path = fileOf(entityType, id);

      // Read the raw frontmatter (not schema-parsed, so existing companions are
      // preserved), then set value + its LWW companions together — one atomic
      // file write, never observable half-applied.
      const raw = parseFrontmatter(await Bun.file(path).text());
      raw[field] = value;
      raw[`${field}_lastModified`] = ts;
      raw[`${field}_lastModifiedBy`] = author;
      await Bun.write(path, serializeEntity(raw));

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "UPDATE_FIELD",
          entityType,
          entityId: id,
          payload: { author, field, value, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }
    },

    async logExisting(entityType, id) {
      const schema = schemaFor(entityType);
      const raw = parseFrontmatter(await Bun.file(fileOf(entityType, id)).text());
      const snapshot = schema.parse(raw) as Record<string, unknown>;

      if (!(await shouldLog())) return;
      const ev: Event = {
        ts: config.clock(),
        op: "CREATE",
        entityType,
        entityId: id,
        payload: { author: config.author(), snapshot, version: config.version },
      };
      await appendFile(logPathOf(), serializeEvent(ev) + "\n");
    },

    async discard(entityType, id) {
      await rm(fileOf(entityType, id), { force: true });
    },

    async createFolder(entityType, opts) {
      const ts = config.clock();
      const author = config.author();

      await mkdir(folderOf(entityType), { recursive: true });

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "CREATE_FOLDER",
          entityType,
          entityId: "",
          payload: { author, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }
    },

    async renameFolder(entityType, to, opts) {
      const ts = config.clock();
      const author = config.author();

      // Renaming the root moves the whole workspace (log included), so the
      // event is appended after the move — i.e. inside the renamed folder.
      if (entityType === ROOT_FOLDER) {
        const moved = join(dirname(root), to);
        await rename(root, moved);
        root = moved;
      } else {
        await rename(folderOf(entityType), folderOf(to));
      }

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "RENAME_FOLDER",
          entityType,
          entityId: "",
          payload: { author, to, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }
    },

    async truncateFolder(entityType, opts) {
      const ts = config.clock();
      const author = config.author();

      await rm(folderOf(entityType), { recursive: true, force: true });
      await mkdir(folderOf(entityType), { recursive: true });

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "TRUNCATE_FOLDER",
          entityType,
          entityId: "",
          payload: { author, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }
    },

    async deleteFolder(entityType, opts) {
      const ts = config.clock();
      const author = config.author();

      await rm(folderOf(entityType), { recursive: true, force: true });

      if (await shouldLog(opts)) {
        const ev: Event = {
          ts,
          op: "DELETE_FOLDER",
          entityType,
          entityId: "",
          payload: { author, version: config.version },
        };
        await appendFile(logPathOf(), serializeEvent(ev) + "\n");
      }
    },

    entityTypes: () => Object.keys(config.schemas),

    version: () => config.version,

    folders() {
      return Object.keys(config.schemas).map(folderNameOf);
    },

    async snapshot() {
      const state: Snapshot = {};
      for (const entityType of Object.keys(config.schemas)) {
        for (const id of await idsIn(entityType)) {
          // Straight from the frontmatter, not through the schema: replay works
          // on stored fields plus their LWW companions, and derives nothing.
          state[`${entityType}/${id}`] = parseFrontmatter(
            await Bun.file(fileOf(entityType, id)).text(),
          );
        }
      }
      return state;
    },

    async materialize(state) {
      for (const [key, fields] of Object.entries(state)) {
        const [entityType, id] = splitKey(key);
        await mkdir(folderOf(entityType), { recursive: true });
        await Bun.write(fileOf(entityType, id), serializeEntity(fields));
      }
    },

    read: readEntity,

    async readAll(entityType) {
      const ids = await idsIn(entityType);
      return await Promise.all(ids.map((id) => readEntity(entityType, id)));
    },

    async purge(threshold) {
      const kept = (await readLog()).filter((ev) => ev.ts > threshold);
      await Bun.write(logPathOf(), serializeLog(kept));
    },

    parseEvents,
    serializeLog,
    serializeEntity,
    parseEntity: parseFrontmatter,

    parseLog: readLog,
  };
}
