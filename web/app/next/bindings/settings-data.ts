// What the Settings screen reads from a machine, as hooks: an agent's settings file (read when its page opens, changed
// one write at a time against the revision the last answer gave), the skills the agent finds, the apps the machine
// reads, whether the runtime has a password sign-in. A page that loads keeps its last answer while it is asked again.
import { useCallback, useEffect, useRef, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { loadApps } from '@/next/features/apps/api';
import {
  loadAgentSettings,
  loadCatalog,
  loadSession,
  patchAgentSettings,
  type ICatalogItem,
} from '@/next/features/settings/api';
import { t } from '@/next/i18n';
import type { AgentSettings, AgentSettingsPatch, AppsFeed, MachineId, Provider } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';

export type Loaded<T> = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; value: T };

const LOADING: Loaded<never> = { k: 'loading' };

/**
 * Asks `load` while `enabled`, again when `sig` (what the request is about) changes and on `retry`. A failure shows only
 * where there is no answer to keep; an answer about something else than `sig` names is not shown.
 */
const useLoaded = <T>(
  enabled: boolean,
  load: () => Promise<{ ok: true; value: T } | Parameters<typeof pageText>[0]>,
  sig: string,
) => {
  const [box, setBox] = useState<{ sig: string; value: Loaded<T> }>({ sig, value: LOADING });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!enabled) return;
    let live = true;
    void load().then((res) => {
      if (!live) return;
      if ('ok' in res) setBox({ sig, value: { k: 'ready', value: res.value } });
      else
        setBox((now) =>
          now.sig === sig && now.value.k === 'ready'
            ? now
            : { sig, value: { k: 'failed', text: pageText(res) } },
        );
    });
    return () => {
      live = false;
    };
  }, [enabled, attempt, sig, load]);

  const retry = useCallback(() => {
    setBox((now) => (now.value.k === 'failed' ? { sig: now.sig, value: LOADING } : now));
    setAttempt((n) => n + 1);
  }, []);
  return { state: box.sig === sig ? box.value : LOADING, retry };
};

export type TPatch = AgentSettingsPatch | ((base: AgentSettings) => AgentSettingsPatch);

export interface IAgentSettingsHandle {
  state: Loaded<AgentSettings>;
  /** Writes the patch to the machine; resolves when it took it and rejects with the reason when it did not. A patch
   *  that depends on the file (a rule added to the rules there) is a function of the file as it stands when its turn
   *  comes. */
  patch: (patch: TPatch) => Promise<void>;
  retry: () => void;
}

/**
 * An agent's settings file. The writes go one after another, each on the revision the one before it gave, so two rows
 * changed at once do not meet as a conflict; a write that does meet one (the file changed on the machine) reloads the
 * file and says so.
 */
export const useAgentSettings = (
  machine: MachineId,
  provider: Provider,
  enabled: boolean,
): IAgentSettingsHandle => {
  const supported = hasFeature(machine, 'settings');
  const [data, setData] = useState<AgentSettings | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const latest = useRef<AgentSettings | null>(null);
  const chain = useRef<Promise<unknown>>(Promise.resolve());

  const take = useCallback((next: AgentSettings) => {
    latest.current = next;
    setData(next);
    setFailure(null);
  }, []);

  useEffect(() => {
    if (!enabled || !supported) return;
    let live = true;
    void loadAgentSettings(machine, provider).then((res) => {
      if (!live) return;
      if ('ok' in res) take(res.value);
      else if (latest.current === null) setFailure(pageText(res));
    });
    return () => {
      live = false;
    };
  }, [enabled, supported, machine, provider, attempt, take]);

  const write = useCallback(
    async (next: TPatch) => {
      const base = latest.current;
      if (!base) throw new Error(t('next.settings.notLoaded'));
      const res = await patchAgentSettings(
        machine,
        provider,
        base.rev,
        typeof next === 'function' ? next(base) : next,
      );
      if ('ok' in res) {
        take(res.value);
        return;
      }
      if (res.r === 'refused' && res.code === 'conflict') {
        setAttempt((n) => n + 1);
        throw new Error(t('next.settings.conflict'));
      }
      throw new Error(pageText(res));
    },
    [machine, provider, take],
  );
  const patch = useCallback(
    (next: TPatch) => {
      const run = chain.current.then(() => write(next));
      chain.current = run.catch(() => undefined);
      return run;
    },
    [write],
  );
  const retry = useCallback(() => {
    setFailure(null);
    setAttempt((n) => n + 1);
  }, []);

  let state: Loaded<AgentSettings> = { k: 'loading' };
  if (enabled && !supported) state = { k: 'failed', text: t('next.settings.unsupported') };
  else if (data) state = { k: 'ready', value: data };
  else if (failure) state = { k: 'failed', text: failure };
  return { state, patch, retry };
};

export type SkillCatalog = Loaded<ICatalogItem[]>;

/** The commands the agent finds on the machine; the page keeps the ones that are skills. */
export const useCatalog = (
  machine: MachineId,
  provider: Provider,
  enabled: boolean,
  cwd: string | null = null,
) =>
  useLoaded<ICatalogItem[]>(
    enabled,
    useCallback(() => loadCatalog(machine, provider, cwd), [machine, provider, cwd]),
    `${machine}/${provider}/${cwd ?? ''}`,
  );

export const useAppsFeed = (machine: MachineId, enabled: boolean) =>
  useLoaded<AppsFeed>(
    enabled && hasFeature(machine, 'apps-hub'),
    useCallback(() => loadApps(machine), [machine]),
    machine,
  );

/** Whether the runtime of the machine signs people in with a password; false until it says so. */
export const useSignIn = (machine: MachineId): boolean => {
  const [password, setPassword] = useState(false);
  useEffect(() => {
    let live = true;
    void loadSession(machine).then((res) => {
      if (live) setPassword('ok' in res && res.value.password);
    });
    return () => {
      live = false;
    };
  }, [machine]);
  return password;
};
