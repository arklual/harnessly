// The data of the machines screen: the list (with the machines a card changed laid over it at once) and one machine's
// card with its actions. An action is optimistic: the card shows the change at once and the answer confirms it; when
// the hub refuses, the card goes back to what it was and says why.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type {
  AccessChange,
  MachineBusyKind,
  MachineDetail,
  MachinesList,
  OrgMachine,
  OwnerHistory,
  RestartWhat,
  RestartWhen,
} from '../../../org/api/machines';
import { useOrg } from '../../../org/context';
import { useLoad } from '../../../org/hooks';
import { seatsOn } from '../../flag';
import { useNow } from '../../../lib/hooks';
import { applyOverlay, countByServers, machineSignals, sizeText } from './calc';

const POLL_MS = 3_000;

export const useMachineList = () => {
  const { scope, shell } = useOrg();
  const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const loaded = useLoad((api) => api.machinesList(shell.org.slug, { scope: slug }), [shell.org.slug, slug]);
  const [over, setOver] = useState<Record<string, OrgMachine>>({});
  // A fresh answer already knows what the card changed: drop the laid-over copies (while rendering, not after).
  const [seen, setSeen] = useState<MachinesList | null>(null);
  if (seen !== loaded.data) {
    setSeen(loaded.data);
    if (Object.keys(over).length > 0) setOver({});
  }
  const seats = seatsOn(shell.features);
  const data = useMemo(() => {
    if (!loaded.data) return null;
    const list = applyOverlay(loaded.data, over);
    return seats ? countByServers(list) : list;
  }, [loaded.data, over, seats]);
  const patch = useCallback((m: OrgMachine) => setOver((o) => ({ ...o, [m.id]: m })), []);
  return { data, error: loaded.error, reload: loaded.reload, refresh: loaded.refresh, patch };
};

/** How many machines of the open scope need a person's step: the count beside «Машины» in the menu. */
export const useMachineAttention = (): number => {
  const { scope, shell } = useOrg();
  const slug = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const loaded = useLoad((api) => api.machinesList(shell.org.slug, { scope: slug }), [shell.org.slug, slug]);
  const now = useNow(60_000);
  return useMemo(
    () => (loaded.data ? machineSignals(loaded.data.groups, now).length : 0),
    [loaded.data, now],
  );
};

const withBusy = (d: MachineDetail, kind: MachineBusyKind): MachineDetail => ({
  ...d,
  machine: { ...d.machine, busy: { kind, chats: d.machine.chats ?? undefined } },
});

const withoutBusy = (d: MachineDetail): MachineDetail => ({ ...d, machine: { ...d.machine, busy: null } });

export interface MachineCard {
  detail: MachineDetail | null;
  /** The card could not be loaded. */
  loadError: unknown;
  reload: () => void;
  /** Why the last action was refused; the card is back to what it was. */
  error: string | null;
  clearError: () => void;
  /** The line after a finished action: «перезапущена только что». */
  done: string | null;
  restart: (what: RestartWhat, when: RestartWhen) => Promise<boolean>;
  cancelRestart: () => Promise<boolean>;
  disable: () => Promise<boolean>;
  enable: () => Promise<boolean>;
  rename: (name: string) => Promise<boolean>;
  /** Takes the machine away; true once the hub has done it. */
  remove: () => Promise<boolean>;
  resize: (to: { cores: number; ramGb: number; diskGb: number }) => Promise<boolean>;
  saveAccess: (change: AccessChange) => Promise<boolean>;
  /** Hands the machine to another member of its space; the answer is the card with the new owner. */
  assignOwner: (userId: string, history: OwnerHistory) => Promise<boolean>;
}

export const useMachineCard = (id: string, onChanged: (m: OrgMachine) => void): MachineCard => {
  const { api } = useOrg();
  const [detail, setDetail] = useState<MachineDetail | null>(null);
  const [loadError, setLoadError] = useState<unknown>(null);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const latest = useRef<MachineDetail | null>(null);
  const flying = useRef(0);
  const alive = useRef(true);
  const changed = useRef(onChanged);
  useEffect(() => {
    changed.current = onChanged;
  });
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const put = useCallback((d: MachineDetail | null) => {
    latest.current = d;
    if (alive.current) setDetail(d);
    if (d) changed.current(d.machine);
  }, []);

  useEffect(() => {
    let live = true;
    api.machineDetail(id).then(
      (d) => {
        if (!live || flying.current > 0) return;
        setLoadError(null);
        latest.current = d;
        setDetail(d);
      },
      (e: unknown) => live && setLoadError(e),
    );
    return () => {
      live = false;
    };
  }, [api, id, tick]);

  // A restart that waits for chats goes on in the hub: look again until it is done.
  const busy = detail?.machine.busy != null;
  useEffect(() => {
    if (!busy) return;
    const t = window.setInterval(() => {
      if (flying.current === 0) setTick((n) => n + 1);
    }, POLL_MS);
    return () => window.clearInterval(t);
  }, [busy]);

  const act = useCallback(
    async (
      optimistic: (d: MachineDetail) => MachineDetail,
      call: () => Promise<MachineDetail>,
      doneText: string | null,
    ): Promise<boolean> => {
      const before = latest.current;
      if (!before) return false;
      setError(null);
      setDone(null);
      flying.current += 1;
      put(optimistic(before));
      try {
        const next = await call();
        put(next);
        if (alive.current) setDone(doneText);
        return true;
      } catch (e) {
        put(before);
        if (alive.current) setError(orgErrorText(e));
        return false;
      } finally {
        flying.current -= 1;
      }
    },
    [put],
  );

  return {
    detail,
    loadError,
    reload: () => {
      setLoadError(null);
      setTick((n) => n + 1);
    },
    error,
    clearError: () => setError(null),
    done,
    restart: (what, when) => {
      const waits = when === 'after_chats' && (latest.current?.machine.chats ?? 0) > 0;
      const kind: MachineBusyKind = waits ? 'drain' : what === 'runner' ? 'restart' : 'reboot';
      return act(
        (d) => withBusy(d, kind),
        () => api.restartMachine(id, what, waits ? 'after_chats' : 'now'),
        waits ? null : 'перезапущена только что',
      );
    },
    cancelRestart: () => act(withoutBusy, () => api.cancelRestart(id), null),
    disable: () =>
      act(
        (d) => withBusy(d, 'off'),
        () => api.disableMachine(id),
        null,
      ),
    enable: () =>
      act(
        (d) => withBusy(d, 'on'),
        () => api.enableMachine(id),
        'подключена обратно',
      ),
    rename: (name) =>
      act(
        (d) => ({ ...d, machine: { ...d.machine, name } }),
        () => api.renameMachine(id, name),
        'название изменено',
      ),
    remove: async () => {
      setError(null);
      flying.current += 1;
      try {
        await api.removeMachine(id);
        return true;
      } catch (e) {
        if (alive.current) setError(orgErrorText(e));
        return false;
      } finally {
        flying.current -= 1;
      }
    },
    assignOwner: (userId, history) =>
      act(
        (d) => d,
        () => api.assignMachineOwner(id, userId, history),
        'владелец изменён',
      ),
    resize: (to) =>
      act(
        (d) => withBusy(d, 'resize'),
        () => api.resizeMachine(id, to),
        `ресурсы изменены: ${sizeText(to)}`,
      ),
    saveAccess: (change) =>
      act(
        (d) => ({
          ...d,
          access: {
            mode: change.mode,
            people:
              change.mode === 'pick'
                ? d.candidates
                    .filter((p) => change.personIds.includes(p.id))
                    .map((p) => (change.levels?.[p.id] === 'view' ? { ...p, level: 'view' as const } : p))
                : d.access.people,
            ...(change.teamLevel ? { teamLevel: change.teamLevel } : {}),
          },
        }),
        () => api.setMachineAccess(id, change),
        null,
      ),
  };
};
