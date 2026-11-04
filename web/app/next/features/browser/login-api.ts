import { getPage, sendBody, type PageOutcome } from '@/next/core/net/http';
import { runtimePath } from '@/next/core/net/machine';
import { pageText } from '@/next/bindings/outcome';
import type { MachineId } from '@/next/protocol/v2';

export interface ILoginAsk {
  id: string;
  conv: string;
  title: string;
  why: string;
  url: string;
  tab: string;
  at: number;
}
const object = (value: unknown): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid login response');
  return value as Record<string, unknown>;
};
const string = (value: unknown): string => {
  if (typeof value !== 'string') throw new Error('Invalid login string');
  return value;
};
export const parseLoginAsk = (value: unknown): ILoginAsk => {
  const ask = object(value);
  if (typeof ask.at !== 'number' || !Number.isFinite(ask.at)) throw new Error('Invalid login time');
  return {
    id: string(ask.id),
    conv: string(ask.conv),
    title: string(ask.title),
    why: string(ask.why),
    url: string(ask.url),
    tab: string(ask.tab),
    at: ask.at,
  };
};
const readAsks = (value: unknown): { asks: ILoginAsk[] } | { error: string } => {
  const body = object(value);
  if ('error' in body) return { error: string(body.error) };
  if (!Array.isArray(body.asks)) throw new Error('Invalid login requests');
  return { asks: body.asks.map(parseLoginAsk) };
};
const readAnswer = (value: unknown): { ok: true } | { error: string } => {
  const body = object(value);
  if ('error' in body) return { error: string(body.error) };
  if (body.ok !== true) throw new Error('Invalid login answer');
  return { ok: true };
};
const valueOf = <T>(result: PageOutcome<T | { error: string }>): T => {
  if (!('ok' in result)) throw new Error(pageText(result));
  if (typeof result.value === 'object' && result.value !== null && 'error' in result.value)
    throw new Error(String(result.value.error));
  return result.value as T;
};
const path = (machine: MachineId) => runtimePath('/api/agent-browser/logins', machine);
export const loadLogins = async (machine: MachineId) =>
  valueOf(await getPage(path(machine), readAsks, 10_000)).asks;
export const answerLogin = async (machine: MachineId, id: string, action: 'done' | 'cancel') => {
  valueOf(await sendBody('POST', path(machine), { id, action }, readAnswer, 25_000));
  window.dispatchEvent(new CustomEvent('browser-logins-changed', { detail: machine }));
};
