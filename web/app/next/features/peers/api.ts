// The runtime's messages between agents (internal/peers), as the page reads and changes them: the section of Settings
// (who may write here, when it reaches a working agent, the audit) and the person's answer to another agent's message.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { PEER_REASONS, type TPeerOutcome, type TPeerReason } from '@/next/features/chat/peer';
import type { MachineId } from '@/next/protocol/v2';

const PEERS_TIMEOUT_MS = 10_000;
/** A message to another machine goes through the gateway and that machine's conversation. */
const SEND_TIMEOUT_MS = 20_000;

export const PEER_SCOPES = ['team', 'list'] as const;
export const PEER_DELIVERIES = ['step', 'turn'] as const;
export const PEER_RULE_KINDS = ['machine', 'person'] as const;

export interface IPeerSender {
  person: string;
  personName: string;
  machine: string;
  machineName: string;
}

export interface IPeerRule {
  kind: (typeof PEER_RULE_KINDS)[number];
  id: string;
  name: string;
  allow: boolean;
}

export interface IPeerSettings {
  accept: boolean;
  scope: (typeof PEER_SCOPES)[number];
  delivery: (typeof PEER_DELIVERIES)[number];
  rules: IPeerRule[];
  rev: number;
}

export interface IPeerParty extends IPeerSender {
  conversationId: string;
  title: string;
}

export interface IPeerRecord {
  id: string;
  at: number;
  dir: 'in' | 'out';
  from: IPeerParty;
  to: IPeerParty;
  text: string;
  state: 'delivered' | 'refused';
  reason: TPeerReason | null;
  via: string;
}

export interface IPeersView {
  self: IPeerSender;
  settings: IPeerSettings;
  /** The machine reaches the fleet's other machines. */
  relay: boolean;
  senders: IPeerSender[];
  audit: IPeerRecord[];
}

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`peers: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const list = (v: unknown, what: string): unknown[] => {
  if (!Array.isArray(v)) throw new Error(`peers: ${what}`);
  return v;
};
const oneOf = <T extends string>(v: unknown, items: readonly T[], what: string): T => {
  const found = items.find((item) => item === v);
  if (found === undefined) throw new Error(`peers: ${what}`);
  return found;
};

const readSender = (v: unknown): IPeerSender => {
  const o = obj(v, 'sender');
  return {
    person: str(o.person),
    personName: str(o.personName),
    machine: str(o.machine),
    machineName: str(o.machineName),
  };
};

const readParty = (v: unknown): IPeerParty => {
  const o = obj(v, 'party');
  return { ...readSender(o), conversationId: str(o.conversationId), title: str(o.title) };
};

const reasonOf = (v: unknown): TPeerReason | null => PEER_REASONS.find((reason) => reason === v) ?? null;

export const readSettings = (v: unknown): IPeerSettings => {
  const o = obj(v, 'settings');
  return {
    accept: o.accept === true,
    scope: oneOf(o.scope, PEER_SCOPES, 'scope'),
    delivery: oneOf(o.delivery, PEER_DELIVERIES, 'delivery'),
    rules: list(o.rules, 'rules').map((raw) => {
      const r = obj(raw, 'rule');
      return {
        kind: oneOf(r.kind, PEER_RULE_KINDS, 'rule kind'),
        id: str(r.id),
        name: str(r.name),
        allow: r.allow === true,
      };
    }),
    rev: typeof o.rev === 'number' ? o.rev : 0,
  };
};

export const readView = (v: unknown): IPeersView => {
  const o = obj(v, 'view');
  return {
    self: readSender(o.self),
    settings: readSettings(o.settings),
    relay: o.relay === true,
    senders: list(o.senders, 'senders').map(readSender),
    audit: list(o.audit, 'audit').map((raw) => {
      const r = obj(raw, 'record');
      return {
        id: str(r.id),
        at: typeof r.at === 'number' ? r.at : 0,
        dir: oneOf(r.dir, ['in', 'out'] as const, 'dir'),
        from: readParty(r.from),
        to: readParty(r.to),
        text: str(r.text),
        state: oneOf(r.state, ['delivered', 'refused'] as const, 'state'),
        reason: reasonOf(r.reason),
        via: str(r.via),
      };
    }),
  };
};

/** A send's answer as what came of it, with the addressee's line when delivered. */
export const readOutcome = (v: unknown): TPeerOutcome => {
  const o = obj(v, 'result');
  if (o.state === 'delivered') {
    const to = o.to ? obj(o.to, 'to') : null;
    const line = to ? [str(to.owner), str(to.machineName), str(to.title)].filter(Boolean).join(' · ') : '';
    return { ok: true, to: line || null };
  }
  return { ok: false, reason: reasonOf(o.reason) };
};

const settingsPath = (machine: MachineId) => runtimePath(platformPath('/peers/settings'), machine);

export const loadPeers = (machine: MachineId) => getPage(settingsPath(machine), readView, PEERS_TIMEOUT_MS);

/** Keeps the settings if they are still at `settings.rev`; the answer is the section as it is now. */
export const savePeers = (machine: MachineId, settings: IPeerSettings) =>
  sendBody('PUT', settingsPath(machine), settings, readView, PEERS_TIMEOUT_MS);

/** The person's own message to another agent's chat, from the chat it answers. */
export const sendPeer = async (
  machine: MachineId,
  from: string,
  to: string,
  text: string,
): Promise<TPeerOutcome> => {
  const res = await sendBody(
    'POST',
    runtimePath(platformPath('/peers/send'), machine),
    { from, to, text },
    readOutcome,
    SEND_TIMEOUT_MS,
  );
  if ('ok' in res) return res.value;
  return { ok: false, reason: res.r === 'failed' && res.code !== 'http' ? 'unreachable' : null };
};
