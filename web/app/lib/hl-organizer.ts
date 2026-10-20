/**
 * The organizer: projects, which project a conversation belongs to, and the ordered pins.
 * One small JSON per user on the fleet's default machine; the shell reads and writes it through
 * GET/PATCH /agents/api/platform/organizer. Everything here is pure so the route and the tests share it.
 */

/** `chat:<openwebui id>` or `agent:<machineId>:<tabId>` (`agent:<tabId>` outside the fleet) — the same string as `Conversation.key`. */
export type TConversationRef = `chat:${string}` | `agent:${string}`;

export const REF_RE = /^(chat:[A-Za-z0-9-]{8,64}|agent:([a-z][a-z0-9-]{0,63}:)?[A-Za-z0-9_-]{1,64})$/;

export const isConversationRef = (value: unknown): value is TConversationRef =>
  typeof value === 'string' && REF_RE.test(value);

/** Project tile colors; each maps to --t-accent-<color> and --t-accent-<color>-bg. */
export const PROJECT_COLORS = ['blue', 'green', 'amber', 'rose', 'purple', 'teal'] as const;
export type TProjectColor = (typeof PROJECT_COLORS)[number];

export const PROJECT_ID_RE = /^prj-[A-Za-z0-9_-]{6,32}$/;

export const ORGANIZER_LIMITS = {
  projects: 100,
  pins: 20,
  assignments: 5000,
  nameLength: 60,
  opsPerRequest: 50,
  fileBytes: 256 * 1024,
} as const;

export interface IOrganizerProject {
  id: string;
  name: string;
  color: TProjectColor;
  createdAt: number;
}

export interface IOrganizer {
  version: 1;
  /** +1 on every PATCH that changed something; the client drops poll answers older than what it holds. */
  revision: number;
  updatedAt: string;
  /** Display order is array order. */
  projects: IOrganizerProject[];
  /** A conversation is in at most one project. */
  assignments: Record<string, string>;
  /** Display order; ⌘1… go to the first visible ones. */
  pins: TConversationRef[];
}

export const emptyOrganizer = (): IOrganizer => ({
  version: 1,
  revision: 0,
  updatedAt: new Date(0).toISOString(),
  projects: [],
  assignments: {},
  pins: [],
});

export type TOrganizerOp =
  | { op: 'project.create'; id: string; name: string; color: TProjectColor; index?: number }
  | { op: 'project.update'; id: string; name?: string; color?: TProjectColor }
  | { op: 'project.delete'; id: string }
  | { op: 'project.reorder'; ids: string[] }
  | { op: 'assign'; refs: TConversationRef[]; projectId: string | null }
  | { op: 'pin'; ref: TConversationRef; index?: number }
  | { op: 'unpin'; ref: TConversationRef }
  | { op: 'pins.reorder'; refs: TConversationRef[] }
  | { op: 'forget'; refs: TConversationRef[] };

export type TOrganizerError =
  | { status: 400; body: { error: 'invalid-op'; index: number; reason: string } }
  | { status: 404; body: { error: 'project-not-found'; id: string } }
  | { status: 409; body: { error: 'project-exists'; id: string } }
  | { status: 413; body: { error: 'limit'; what: 'projects' | 'pins' | 'assignments' } };

export type TApplyResult =
  { ok: true; organizer: IOrganizer; changed: boolean } | { ok: false; error: TOrganizerError };

const invalid = (index: number, reason: string): TOrganizerError => ({
  status: 400,
  body: { error: 'invalid-op', index, reason },
});

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);
const isColor = (value: unknown): value is TProjectColor =>
  typeof value === 'string' && (PROJECT_COLORS as readonly string[]).includes(value);
const isIndex = (value: unknown): value is number => Number.isInteger(value) && (value as number) >= 0;

/** Trimmed name, or null when it is empty or too long. */
export const normalizeProjectName = (value: unknown): string | null => {
  if (typeof value !== 'string') return null;
  const name = value.replace(/\s+/g, ' ').trim();
  return name && name.length <= ORGANIZER_LIMITS.nameLength ? name : null;
};

const refList = (value: unknown): TConversationRef[] | null =>
  Array.isArray(value) &&
  value.length > 0 &&
  value.length <= ORGANIZER_LIMITS.assignments &&
  value.every(isConversationRef)
    ? value
    : null;

/** Validates the shape of one op; the reason is for the developer, the UI shows its own text. */
export const parseOrganizerOp = (
  raw: unknown,
  index: number,
): { ok: true; op: TOrganizerOp } | { ok: false; error: TOrganizerError } => {
  if (!isRecord(raw)) return { ok: false, error: invalid(index, 'op must be an object') };
  const fail = (reason: string) => ({ ok: false as const, error: invalid(index, reason) });
  switch (raw.op) {
    case 'project.create': {
      const name = normalizeProjectName(raw.name);
      if (typeof raw.id !== 'string' || !PROJECT_ID_RE.test(raw.id)) return fail('bad project id');
      if (!name) return fail('bad name');
      if (!isColor(raw.color)) return fail('bad color');
      if (raw.index !== undefined && !isIndex(raw.index)) return fail('bad index');
      return {
        ok: true,
        op: {
          op: 'project.create',
          id: raw.id,
          name,
          color: raw.color,
          index: raw.index as number | undefined,
        },
      };
    }
    case 'project.update': {
      if (typeof raw.id !== 'string' || !PROJECT_ID_RE.test(raw.id)) return fail('bad project id');
      const name = raw.name === undefined ? undefined : normalizeProjectName(raw.name);
      if (name === null) return fail('bad name');
      if (raw.color !== undefined && !isColor(raw.color)) return fail('bad color');
      if (name === undefined && raw.color === undefined) return fail('nothing to update');
      return {
        ok: true,
        op: { op: 'project.update', id: raw.id, name, color: raw.color as TProjectColor | undefined },
      };
    }
    case 'project.delete':
      if (typeof raw.id !== 'string' || !PROJECT_ID_RE.test(raw.id)) return fail('bad project id');
      return { ok: true, op: { op: 'project.delete', id: raw.id } };
    case 'project.reorder':
      if (!Array.isArray(raw.ids) || !raw.ids.every((id) => typeof id === 'string'))
        return fail('ids must be strings');
      return { ok: true, op: { op: 'project.reorder', ids: raw.ids as string[] } };
    case 'assign': {
      const refs = refList(raw.refs);
      if (!refs) return fail('bad refs');
      if (raw.projectId !== null && (typeof raw.projectId !== 'string' || !PROJECT_ID_RE.test(raw.projectId)))
        return fail('bad project id');
      return { ok: true, op: { op: 'assign', refs, projectId: raw.projectId as string | null } };
    }
    case 'pin':
      if (!isConversationRef(raw.ref)) return fail('bad ref');
      if (raw.index !== undefined && !isIndex(raw.index)) return fail('bad index');
      return { ok: true, op: { op: 'pin', ref: raw.ref, index: raw.index as number | undefined } };
    case 'unpin':
      if (!isConversationRef(raw.ref)) return fail('bad ref');
      return { ok: true, op: { op: 'unpin', ref: raw.ref } };
    case 'pins.reorder':
      if (!Array.isArray(raw.refs) || !raw.refs.every(isConversationRef)) return fail('bad refs');
      return { ok: true, op: { op: 'pins.reorder', refs: raw.refs as TConversationRef[] } };
    case 'forget': {
      const refs = refList(raw.refs);
      if (!refs) return fail('bad refs');
      return { ok: true, op: { op: 'forget', refs } };
    }
    default:
      return fail('unknown op');
  }
};

export const parseOrganizerOps = (
  body: unknown,
): { ok: true; ops: TOrganizerOp[] } | { ok: false; error: TOrganizerError } => {
  const ops = isRecord(body) ? body.ops : undefined;
  if (!Array.isArray(ops) || ops.length === 0 || ops.length > ORGANIZER_LIMITS.opsPerRequest) {
    return { ok: false, error: invalid(-1, `ops must be an array of 1..${ORGANIZER_LIMITS.opsPerRequest}`) };
  }
  const parsed: TOrganizerOp[] = [];
  for (const [index, raw] of ops.entries()) {
    const result = parseOrganizerOp(raw, index);
    if (!result.ok) return result;
    parsed.push(result.op);
  }
  return { ok: true, ops: parsed };
};

const isPermutation = <T>(next: readonly T[], current: readonly T[]) =>
  next.length === current.length &&
  new Set(next).size === next.length &&
  next.every((item) => current.includes(item));

const insertAt = <T>(list: T[], item: T, index: number | undefined) => {
  const at = index === undefined ? list.length : Math.min(index, list.length);
  list.splice(at, 0, item);
};

const clone = (organizer: IOrganizer): IOrganizer => ({
  ...organizer,
  projects: organizer.projects.map((project) => ({ ...project })),
  assignments: { ...organizer.assignments },
  pins: [...organizer.pins],
});

/**
 * Applies every op to a copy, all or nothing. `changed` is false when the result equals the input:
 * the route then keeps the revision and skips the write, so a repeated create is a true no-op.
 */
export const applyOrganizerOps = (
  current: IOrganizer,
  ops: readonly TOrganizerOp[],
  now = Date.now(),
): TApplyResult => {
  const next = clone(current);
  for (const [index, op] of ops.entries()) {
    const project = 'id' in op ? next.projects.find((p) => p.id === op.id) : undefined;
    switch (op.op) {
      case 'project.create':
        if (project) {
          if (project.name !== op.name)
            return { ok: false, error: { status: 409, body: { error: 'project-exists', id: op.id } } };
          break;
        }
        if (next.projects.length >= ORGANIZER_LIMITS.projects)
          return { ok: false, error: { status: 413, body: { error: 'limit', what: 'projects' } } };
        insertAt(next.projects, { id: op.id, name: op.name, color: op.color, createdAt: now }, op.index);
        break;
      case 'project.update':
        if (!project)
          return { ok: false, error: { status: 404, body: { error: 'project-not-found', id: op.id } } };
        if (op.name !== undefined) project.name = op.name;
        if (op.color !== undefined) project.color = op.color;
        break;
      case 'project.delete':
        if (!project)
          return { ok: false, error: { status: 404, body: { error: 'project-not-found', id: op.id } } };
        next.projects = next.projects.filter((p) => p.id !== op.id);
        for (const [ref, projectId] of Object.entries(next.assignments))
          if (projectId === op.id) delete next.assignments[ref];
        break;
      case 'project.reorder': {
        const ids = next.projects.map((p) => p.id);
        if (!isPermutation(op.ids, ids))
          return { ok: false, error: invalid(index, 'ids must be a permutation of all projects') };
        next.projects = op.ids.map((id) => next.projects.find((p) => p.id === id)!);
        break;
      }
      case 'assign':
        if (op.projectId !== null && !next.projects.some((p) => p.id === op.projectId)) {
          return {
            ok: false,
            error: { status: 404, body: { error: 'project-not-found', id: op.projectId } },
          };
        }
        for (const ref of op.refs) {
          if (op.projectId === null) delete next.assignments[ref];
          else next.assignments[ref] = op.projectId;
        }
        if (Object.keys(next.assignments).length > ORGANIZER_LIMITS.assignments)
          return { ok: false, error: { status: 413, body: { error: 'limit', what: 'assignments' } } };
        break;
      case 'pin': {
        const without = next.pins.filter((ref) => ref !== op.ref);
        if (without.length >= ORGANIZER_LIMITS.pins)
          return { ok: false, error: { status: 413, body: { error: 'limit', what: 'pins' } } };
        insertAt(without, op.ref, op.index);
        next.pins = without;
        break;
      }
      case 'unpin':
        next.pins = next.pins.filter((ref) => ref !== op.ref);
        break;
      case 'pins.reorder':
        if (!isPermutation(op.refs, next.pins))
          return { ok: false, error: invalid(index, 'refs must be a permutation of all pins') };
        next.pins = [...op.refs];
        break;
      case 'forget':
        next.pins = next.pins.filter((ref) => !op.refs.includes(ref));
        for (const ref of op.refs) delete next.assignments[ref];
        break;
    }
  }
  const changed =
    JSON.stringify({ ...next, revision: 0, updatedAt: '' }) !==
    JSON.stringify({ ...current, revision: 0, updatedAt: '' });
  if (!changed) return { ok: true, organizer: current, changed: false };
  return {
    ok: true,
    organizer: { ...next, revision: current.revision + 1, updatedAt: new Date(now).toISOString() },
    changed: true,
  };
};

/** Structural check of a file read from disk; anything else is "unreadable" and never overwritten. */
export const isOrganizer = (value: unknown): value is IOrganizer => {
  if (
    !isRecord(value) ||
    value.version !== 1 ||
    !Number.isInteger(value.revision) ||
    typeof value.updatedAt !== 'string'
  )
    return false;
  if (!Array.isArray(value.projects) || !Array.isArray(value.pins) || !isRecord(value.assignments))
    return false;
  const projectsOk = value.projects.every(
    (p) =>
      isRecord(p) &&
      typeof p.id === 'string' &&
      typeof p.name === 'string' &&
      isColor(p.color) &&
      typeof p.createdAt === 'number',
  );
  return (
    projectsOk &&
    value.pins.every(isConversationRef) &&
    Object.values(value.assignments).every((id) => typeof id === 'string')
  );
};

/** Project of a new tile: cycles through the colors so neighbours differ. */
export const nextProjectColor = (projects: readonly IOrganizerProject[]): TProjectColor =>
  PROJECT_COLORS[projects.length % PROJECT_COLORS.length];
