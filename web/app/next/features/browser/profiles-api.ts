import { getPage, sendBody, type PageOutcome } from '@/next/core/net/http';
import { runtimePath } from '@/next/core/net/machine';
import { pageText } from '@/next/bindings/outcome';
import type { MachineId } from '@/next/protocol/v2';

export interface IProfileChoice {
  id: string;
  name: string;
  color: string;
  kind: string;
}
export interface IBrowserProfile extends IProfileChoice {
  createdAt: number;
  lastUsedAt: number;
  usedBy: { conv: string; title: string; at: number }[];
  path: string;
  running: boolean;
  projects: string[];
}
export interface IProfileAccess {
  default: string;
  granted: string[];
}
export interface IProfileAsk {
  id: string;
  conv: string;
  title: string;
  why: string;
  at: number;
}
export interface IProfilesPage {
  profiles: IBrowserProfile[];
  projects: Record<string, IProfileAccess>;
  asks: IProfileAsk[];
  noSandbox: boolean;
}
export interface IConversationProfiles {
  profiles: IProfileChoice[];
  current: IProfileChoice;
  hasTabs: boolean;
}
export type TProfileAction =
  | { action: 'create'; name: string; color: string }
  | { action: 'update'; id: string; name: string; color: string }
  | { action: 'stop' | 'clear' | 'delete'; id: string }
  | { action: 'import'; name: string; color: string; path: string }
  | { action: 'sandbox'; noSandbox: boolean }
  | { action: 'project'; project: string; default: string; granted: string[] }
  | { action: 'use'; conv: string; id: string }
  | {
      action: 'answer';
      askId: string;
      id: string;
      scope: 'chat' | 'project' | 'decline';
      name?: string;
      color?: string;
    };
type Answer<T> = T | { error: string };
const object = (value: unknown): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid browser profiles response');
  return value as Record<string, unknown>;
};
const string = (value: unknown): string => {
  if (typeof value !== 'string') throw new Error('Invalid browser profile string');
  return value;
};
const number = (value: unknown): number => {
  if (typeof value !== 'number' || !Number.isFinite(value)) throw new Error('Invalid browser profile time');
  return value;
};
const boolean = (value: unknown): boolean => {
  if (typeof value !== 'boolean') throw new Error('Invalid browser profile flag');
  return value;
};
const list = <T>(value: unknown, parse: (v: unknown) => T): T[] => {
  if (value === null) return [];
  if (!Array.isArray(value)) throw new Error('Invalid browser profile list');
  return value.map(parse);
};
const choice = (value: unknown): IProfileChoice => {
  const p = object(value);
  return { id: string(p.id), name: string(p.name), color: string(p.color), kind: string(p.kind) };
};
const profile = (value: unknown): IBrowserProfile => {
  const p = object(value);
  return {
    ...choice(value),
    createdAt: number(p.createdAt),
    lastUsedAt: number(p.lastUsedAt),
    path: string(p.path),
    running: boolean(p.running),
    projects: list(p.projects, string),
    usedBy: list(p.usedBy, (v) => {
      const use = object(v);
      return { conv: string(use.conv), title: string(use.title), at: number(use.at) };
    }),
  };
};
const read =
  <T>(parse: (o: Record<string, unknown>) => T) =>
  (value: unknown): Answer<T> => {
    const o = object(value);
    return 'error' in o ? { error: string(o.error) } : parse(o);
  };
const readProfiles = read<IProfilesPage>((o) => ({
  profiles: list(o.profiles, profile),
  noSandbox: boolean(o.noSandbox),
  projects: Object.fromEntries(
    Object.entries(object(o.projects)).map(([id, value]) => {
      const access = object(value);
      return [id, { default: string(access.default), granted: list(access.granted, string) }];
    }),
  ),
  asks: list(o.asks, (value) => {
    const ask = object(value);
    return {
      id: string(ask.id),
      conv: string(ask.conv),
      title: string(ask.title),
      why: string(ask.why),
      at: number(ask.at),
    };
  }),
}));
const readConversation = read<IConversationProfiles>((o) => ({
  profiles: list(o.profiles, choice),
  current: choice(o.current),
  hasTabs: boolean(o.hasTabs),
}));
const readOk = read((o) => {
  if (o.ok !== true) throw new Error('Invalid browser profile mutation response');
  return { ok: true, warning: o.warning == null ? '' : string(o.warning) };
});
const valueOf = <T>(res: PageOutcome<Answer<T>>): T => {
  if (!('ok' in res)) throw new Error(pageText(res));
  if (typeof res.value === 'object' && res.value !== null && 'error' in res.value)
    throw new Error(String(res.value.error));
  return res.value as T;
};
const path = (machine: MachineId, conv?: string) =>
  runtimePath(`/api/agent-browser/profiles${conv ? `?conv=${encodeURIComponent(conv)}` : ''}`, machine);
export const loadProfiles = async (machine: MachineId): Promise<IProfilesPage> =>
  valueOf(await getPage(path(machine), readProfiles, 10_000));
export const loadConversationProfiles = async (
  machine: MachineId,
  conv: string,
): Promise<IConversationProfiles> => valueOf(await getPage(path(machine, conv), readConversation, 10_000));
export const changeProfile = async (
  machine: MachineId,
  action: TProfileAction,
): Promise<{ warning: string }> => {
  const answer = valueOf(await sendBody('POST', path(machine), action, readOk, 30_000));
  window.dispatchEvent(new CustomEvent('browser-profiles-changed', { detail: machine }));
  return { warning: answer.warning };
};
