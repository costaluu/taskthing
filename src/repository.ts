import type { Mdwal } from "./mdwal";

// ── Repository<Model> ───────────────────────────────────────────────────────
//
// A thin adapter over mdwal, one per entity type: model objects in, model
// objects out. Higher layers work with these instead of raw mdwal operations.

/**
 * `Input` is what a caller must supply to create one: by default every field
 * but the id, though a schema with defaults accepts less than it stores — pass
 * the schema's input type to say so.
 */
export interface Repository<Model, Input = Omit<Model, "id">> {
  create(model: Input): Promise<Model | null>;
  /**
   * Persist a model by writing one atomic field event per *changed* field.
   * Fields the caller passed back unchanged are left alone, so they can't win an
   * LWW race against a concurrent peer edit they never meant to overwrite.
   */
  update(model: Model): Promise<boolean>;
  /** Soft-delete: sets `deleted = true`; the entity stays retrievable. */
  delete(id: string): Promise<boolean>;
  /** Undo a soft-delete through the same field-update mechanism. */
  recover(id: string): Promise<boolean>;
  findById(id: string): Promise<Model | null>;
  /** Every entity of the type, soft-deleted ones included. */
  findAll(): Promise<Model[]>;
  /** An in-memory predicate over every materialized model. */
  filter(predicate: (value: Model) => boolean): Promise<Model[]>;
}

/**
 * Whether a field's new value is the same as its stored one. Compared by
 * content: a caller who reads a model and writes it back hands us fresh arrays
 * and objects holding identical values, and re-logging those would enter an LWW
 * race against a peer's genuine edit. Field values are schema-validated JSON, so
 * their serialization is a faithful and stable comparison.
 */
function unchanged(next: unknown, stored: unknown): boolean {
  if (next === stored) return true;
  if (typeof next !== "object" || typeof stored !== "object") return false;
  return JSON.stringify(next) === JSON.stringify(stored);
}

export function createRepository<Model extends { id: string }, Input = Omit<Model, "id">>(
  mdwal: Mdwal,
  entityType: string,
): Repository<Model, Input> {
  // A missing entity is a miss, not a failure: the caller asked whether one
  // exists, and the answer is no.
  async function findById(id: string): Promise<Model | null> {
    try {
      return (await mdwal.read(entityType, id)) as Model;
    } catch {
      return null;
    }
  }

  // Delete and recover are the same op with opposite values: soft-delete is an
  // ordinary field update, so it is subject to LWW like any other edit.
  async function setDeleted(id: string, value: boolean): Promise<boolean> {
    if ((await findById(id)) === null) return false;
    await mdwal.updateField(entityType, id, "deleted", value);
    return true;
  }

  async function findAll(): Promise<Model[]> {
    return (await mdwal.readAll(entityType)) as Model[];
  }

  return {
    async create(model) {
      return (await mdwal.createEntity(
        entityType,
        model as Record<string, unknown>,
      )) as Model;
    },

    async update(model) {
      const current = await findById(model.id);
      if (current === null) return false;

      for (const [field, value] of Object.entries(model)) {
        if (field === "id") continue;
        if (unchanged(value, (current as Record<string, unknown>)[field])) continue;
        await mdwal.updateField(entityType, model.id, field, value);
      }
      return true;
    },

    delete: (id) => setDeleted(id, true),
    recover: (id) => setDeleted(id, false),
    findById,
    findAll,

    async filter(predicate) {
      return (await findAll()).filter(predicate);
    },
  };
}
