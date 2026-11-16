/* eslint-disable @typescript-eslint/no-explicit-any -- the page's own window and the bridge core are untyped */
/**
 * The agent's actions on a site open in the desktop app's own pane browser. The shell cannot wait on a
 * promise in the page, so an action is started and its outcome read back later: `start(message)` answers at once, and
 * `take(id)` answers the outcome as JSON text once there is one (null before). Outcomes are kept for a short while
 * and a few at most: the shell takes each one once.
 */
import { parseActMessage } from './bridge-core';

const KEEP = 16;
const TEXT_LIMIT = 64 * 1024;

export interface INativeActs {
  start: (message: unknown) => boolean;
  take: (id: string) => string | null;
}

/** The site the person let the agent act on is the page's: the same check the machine's live browser makes. */
export const offSite = (href: string, host: string | undefined): string | null => {
  if (!host) return null;
  let at = '';
  try {
    at = new URL(href).hostname;
  } catch {
    // An address that does not parse is not the allowed site.
  }
  if (at.toLowerCase() === host.toLowerCase()) return null;
  return `The page is on another site now (${at || 'unknown'}), not ${host}: the person has to allow it; take a snapshot.`;
};

export const installNativeActs = (win: any, doc: any, core: any): INativeActs | null => {
  if (win.__hlNativeAct) return null;
  const siteTools = core.makeSiteTools();
  const bound = { ...core.captureNatives(win), origin: win.location.origin, parent: win, siteTools };
  core.installModelContext(win, siteTools);
  const refs = core.makeActRefs();
  // Taken before the site's scripts run: the site cannot swap them to read or forge outcomes.
  const stringify = JSON.stringify;
  const parse = JSON.parse;
  const done = new Map<string, string>();
  const keep = (id: string, outcome: { ok: boolean; text: string; rect?: unknown }) => {
    const text = String(outcome.text ?? '').slice(0, TEXT_LIMIT);
    done.set(id, stringify({ id, ok: outcome.ok === true, text, rect: outcome.rect ?? null }));
    while (done.size > KEEP) done.delete(done.keys().next().value as string);
  };
  const failed = (error: any) => ({
    ok: false,
    text: `The page got in the way of the action: ${String(error?.message ?? error).slice(0, 200)}`,
  });
  const acts: INativeActs = Object.freeze({
    start: (given: unknown) => {
      let message: Record<string, unknown>;
      try {
        message = typeof given === 'string' ? parse(given) : (given as Record<string, unknown>);
      } catch {
        return false;
      }
      if (typeof message !== 'object' || message === null || Array.isArray(message)) return false;
      const act = parseActMessage({ ...message, type: 'tk:act' });
      if (!act) return false;
      const away = offSite(
        String(win.location.href),
        typeof message.host === 'string' ? message.host : undefined,
      );
      if (away !== null) {
        keep(act.id, { ok: false, text: away });
        return true;
      }
      try {
        core.runAct(win, doc, bound, core, refs, act).then(
          (outcome: any) => keep(act.id, outcome),
          (error: any) => keep(act.id, failed(error)),
        );
      } catch (error) {
        keep(act.id, failed(error));
      }
      return true;
    },
    take: (id: string) => {
      const outcome = done.get(String(id));
      if (outcome === undefined) return null;
      done.delete(String(id));
      return outcome;
    },
  });
  Object.defineProperty(win, '__hlNativeAct', {
    value: acts,
    configurable: false,
    writable: false,
    enumerable: false,
  });
  return acts;
};
