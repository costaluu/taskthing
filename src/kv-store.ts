import { mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";

// ── kv_store (ephemeral number ↔ nanoid) ────────────────────────────────────
//
// Nobody wants to type `taskthing star V1StGXR8_Z5jdHi6B-myT`. A listing hands
// out small numbers and records what they point at, so the next command can
// resolve `3` back to its entity.
//
// The numbers are deliberately short-lived: every listing resets them, and two
// terminals listing the same workspace simply overwrite each other — the most
// recent listing wins. They are an ergonomic reference, never an identity.
//
// taskthing is a short-lived CLI with no state between invocations, so the map
// lives on disk and is read fresh every time, never cached in memory. It is
// derived local state, so it lives under the gitignored metadata folder.

export type StoreKind = "tasks" | "boards" | "migrations";

export interface KvStore {
  /** The nanoid a number refers to, or null when the number is unknown. */
  get(number: number): Promise<string | null>;
  /** The number an entity currently answers to, or null when unlisted. */
  numberOf(id: string): Promise<number | null>;
  /** Assign the next free number to an entity, answering with it. */
  set(id: string): Promise<number>;
  /** Forget every number, as a fresh listing does before repopulating. */
  reset(): Promise<void>;
}

/**
 * The store a `--global` listing fills: it spans workspaces, so it lives beside
 * them in the data home rather than inside any one of them, and its values are
 * `<workspace>/<nanoid>` — a number alone would be ambiguous across workspaces.
 */
export function createGlobalKvStore(dataHome: string): KvStore {
  return createStore(join(dataHome, ".kv", "global.json"));
}

export function createKvStore(root: string, kind: StoreKind): KvStore {
  return createStore(join(root, ".taskthing", "kv", `${kind}.json`));
}

function createStore(path: string): KvStore {
  async function load(): Promise<Record<string, string>> {
    try {
      return JSON.parse(await Bun.file(path).text()) as Record<string, string>;
    } catch {
      return {};
    }
  }

  async function save(entries: Record<string, string>): Promise<void> {
    await mkdir(dirname(path), { recursive: true });
    await Bun.write(path, JSON.stringify(entries));
  }

  return {
    async get(number) {
      return (await load())[String(number)] ?? null;
    },

    async numberOf(id) {
      const found = Object.entries(await load()).find(([, value]) => value === id);
      return found === undefined ? null : Number(found[0]);
    },

    async set(id) {
      const entries = await load();
      const next = Object.keys(entries).length + 1;
      entries[String(next)] = id;
      await save(entries);
      return next;
    },

    async reset() {
      await save({});
    },
  };
}
