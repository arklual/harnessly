// «Продолжить на другой ВМ» on the cluster's gateway: whether the switch is on, where a chat can go, the
// move itself. The gateway checks the person's own machines and talks to them; the page only asks and shows its words.
// Standalone there is no gateway: the status then reads as «off» and the menu offers nothing.
import { clusterApi, gatewayPath } from '@/next/core/net/machine';
import { t } from '@/next/i18n';

export interface IMoveCheck {
  id: string;
  ok: boolean;
  code?: string;
}

export interface IMoveModel {
  id: string;
  label: string;
  default: boolean;
}

/** A way to continue on a machine: its CLI, or a vendor's profile through it. */
export interface IMoveAgent {
  provider: 'claude' | 'codex';
  profile: string;
  short: string;
  models: readonly IMoveModel[];
}

export interface IMoveTarget {
  machineId: string;
  name: string;
  /** Only from the fleet hub: the organization the machine belongs to. */
  orgName?: string;
  online: boolean;
  /** The machine can take the chat: it is up, its agent and model are there. */
  ok: boolean;
  code?: string;
  checks: readonly IMoveCheck[];
  /** The project's files can come along: the repository, the commit and a clean tree are there. */
  filesOk: boolean;
  agents: readonly IMoveAgent[];
}

export interface IMoveSource {
  ok: boolean;
  code?: string;
  project: boolean;
  dirty: boolean;
  busy: boolean;
}

export interface IMoveTargets {
  source: IMoveSource;
  targets: readonly IMoveTarget[];
}

/** Where a moved chat went on. */
export interface IMoved {
  toMachine: string;
  toName: string;
  toConv: string;
  at: number | null;
}

export interface IMoveStatus {
  enabled: boolean;
  moved: IMoved | null;
}

export interface IMoveRequest {
  opId: string;
  machineId: string;
  convId: string;
  targetMachineId: string;
  provider: 'claude' | 'codex';
  profile?: string;
  model: { id: string };
  withoutFiles: boolean;
}

export interface IMoveResult {
  targetMachineId: string;
  targetName: string;
  convId: string;
}

/** The gateway's refusal in its own words, with its code. */
export class MoveError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number,
  ) {
    super(message);
  }
}

const TIMEOUT_MS = 20_000;
/** The move itself waits for two machines and a worktree. */
const MOVE_TIMEOUT_MS = 150_000;

/** Who answers a machine's moves: the fleet hub (runtime.fleet, the same path under /api/v2) or the cluster's gateway.
 *  Learned from the first status, so without the flag the page goes on asking the gateway as before. */
const hubRoute = new Map<string, boolean>();

const send = async (
  method: 'GET' | 'POST',
  path: `/${string}`,
  body?: unknown,
  timeoutMs = TIMEOUT_MS,
  hub = false,
): Promise<unknown> => {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  let res: Response;
  try {
    res = await fetch(hub ? clusterApi(path) : gatewayPath(path), {
      method,
      credentials: 'same-origin',
      cache: 'no-store',
      signal: ctl.signal,
      ...(body === undefined
        ? {}
        : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    });
  } catch {
    throw new MoveError(
      ctl.signal.aborted ? t('next.moveVm.timeout') : t('next.moveVm.network'),
      'network',
      0,
    );
  } finally {
    clearTimeout(timer);
  }
  const answer: unknown = await res.json().catch(() => null);
  if (res.ok) return answer;
  const said =
    typeof answer === 'object' && answer !== null ? (answer as { error?: unknown; code?: unknown }) : {};
  throw new MoveError(
    typeof said.error === 'string' && said.error ? said.error : t('next.moveVm.failed'),
    typeof said.code === 'string' ? said.code : 'unknown',
    res.status,
  );
};

const query = (machine: string, conv?: string): string =>
  `machine=${encodeURIComponent(machine)}${conv ? `&conv=${encodeURIComponent(conv)}` : ''}`;

const isStatus = (v: unknown): v is IMoveStatus =>
  typeof v === 'object' && v !== null && typeof (v as { enabled?: unknown }).enabled === 'boolean';

/** The switch of the machine, and (with a chat) where the chat went; «off, not moved» where there is no answer. The
 *  fleet hub is asked first; a hub without the flag (or standalone) has no such path, then the gateway answers. */
export const fetchMoveStatus = async (machine: string, conv?: string): Promise<IMoveStatus> => {
  for (const hub of hubRoute.get(machine) === false ? [false] : [true, false]) {
    try {
      const answer = await send(
        'GET',
        `/chat-move/status?${query(machine, conv)}`,
        undefined,
        TIMEOUT_MS,
        hub,
      );
      if (!isStatus(answer)) continue;
      hubRoute.set(machine, hub);
      return { enabled: answer.enabled, moved: answer.moved ?? null };
    } catch {
      if (!hub) break;
    }
  }
  hubRoute.set(machine, false);
  return { enabled: false, moved: null };
};

export const fetchMoveTargets = async (machine: string, conv: string): Promise<IMoveTargets> => {
  const answer = await send(
    'GET',
    `/chat-move/targets?${query(machine, conv)}`,
    undefined,
    TIMEOUT_MS,
    hubRoute.get(machine),
  );
  if (typeof answer !== 'object' || answer === null || !('targets' in answer) || !('source' in answer)) {
    throw new MoveError(t('next.moveVm.failed'), 'unknown', 200);
  }
  return answer as IMoveTargets;
};

export const postMove = async (req: IMoveRequest): Promise<IMoveResult> => {
  const answer = await send('POST', '/chat-move', req, MOVE_TIMEOUT_MS, hubRoute.get(req.machineId));
  const said = typeof answer === 'object' && answer !== null ? (answer as Partial<IMoveResult>) : {};
  if (typeof said.convId !== 'string' || typeof said.targetMachineId !== 'string') {
    throw new MoveError(t('next.moveVm.failed'), 'unknown', 200);
  }
  return {
    targetMachineId: said.targetMachineId,
    targetName: said.targetName ?? said.targetMachineId,
    convId: said.convId,
  };
};

const ENABLED_MS = 60_000;
const enabledCache = new Map<string, { at: number; value: Promise<boolean> }>();

/** Whether the switch is on for a machine, asked once a minute however many menus want it. */
export const moveEnabled = (machine: string): Promise<boolean> => {
  const hit = enabledCache.get(machine);
  if (hit && Date.now() - hit.at < ENABLED_MS) return hit.value;
  const value = fetchMoveStatus(machine).then((s) => s.enabled);
  enabledCache.set(machine, { at: Date.now(), value });
  return value;
};
