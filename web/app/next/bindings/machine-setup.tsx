// The start page's «Настройка машины»: how the team's environment list is set up on the machine a new chat
// starts on, read every few seconds while the page is visible, with «Повторить» and «Открыть сейф». A card that was
// hidden stays hidden until the list changes; a card goes away, fading, once everything is done.
import { useEffect, useEffectEvent, useMemo, useState } from 'react';
import { pageClock } from '@/next/core/clock';
import {
  askBaseProxy,
  askBaseRun,
  askBaseSkip,
  askSetupRun,
  loadBase,
  loadSetup,
  withBase,
  type IBaseView,
  type ISetupView,
} from '@/next/features/setup/api';
import { ACTION, cardOf, type TAction } from '@/next/features/setup/card-model';
import SetupCard from '@/next/features/setup/setup-card';
import type { MachineId } from '@/next/protocol/v2';
import { usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const SETUP_POLL_MS = 4000;
/** «Машина настроена» stays this long after the last step is done, then the card leaves. */
export const DONE_SHOWN_MS = 4000;
const EXIT_MS = 240;
const HIDDEN_KEY = 'hx.setup.hidden';

const readHidden = (): string => {
  try {
    return localStorage.getItem(HIDDEN_KEY) ?? '';
  } catch {
    return '';
  }
};
const writeHidden = (hash: string) => {
  try {
    localStorage.setItem(HIDDEN_KEY, hash);
  } catch {
    // A private window: the card is hidden for this page only.
  }
};

interface ISeen {
  machine: MachineId;
  view: ISetupView;
  /** The view was not done when this page first saw the list: «Машина настроена» is news. */
  wasOpen: boolean;
}

export const MachineSetup = ({
  machine,
  onOpenSafe,
}: {
  machine: MachineId | null;
  onOpenSafe: () => void;
}) => {
  const [seen, setSeen] = useState<ISeen | null>(null);
  const [hidden, setHidden] = useState(readHidden);
  const [picked, setPicked] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [doneGone, setDoneGone] = useState('');
  const [proxy, setProxy] = useState('');
  // A change the person made: look again now, not at the next tick.
  const [nudge, setNudge] = useState(0);

  const look = useEffectEvent(async (m: MachineId) => {
    // The base package is read beside the hub's list: a runtime without it (older) answers 404 and is left out.
    const [res, baseRes] = await Promise.all([loadSetup(m), loadBase(m)]);
    const setup = 'ok' in res ? res.value : null;
    const base: IBaseView | null = 'ok' in baseRes ? baseRes.value : null;
    if (base) setProxy(base.proxy);
    const view = withBase(setup, base);
    if (!view) return;
    setSeen((now) => {
      const same = now?.machine === m && now.view.hash === view.hash;
      const wasOpen = same ? now.wasOpen || !view.done : !view.done;
      if (same && JSON.stringify(now.view) === JSON.stringify(view) && now.wasOpen === wasOpen) return now;
      return { machine: m, view, wasOpen };
    });
  });

  useEffect(() => {
    if (!machine) return;
    const clock = pageClock();
    let timer: unknown = null;
    let looking = false;
    const again = () => {
      clock.clearTimeout(timer);
      timer = clock.setTimeout(again, SETUP_POLL_MS);
      if (looking || document.visibilityState !== 'visible') return;
      looking = true;
      void look(machine)
        .catch(() => undefined)
        .finally(() => {
          looking = false;
        });
    };
    again();
    return () => clock.clearTimeout(timer);
  }, [machine, nudge]);

  const view = seen && seen.machine === machine ? seen.view : null;
  const doneKey = view?.done ? view.hash : '';
  useEffect(() => {
    if (!doneKey) return;
    const clock = pageClock();
    const timer = clock.setTimeout(() => setDoneGone(doneKey), DONE_SHOWN_MS);
    return () => clock.clearTimeout(timer);
  }, [doneKey]);

  const showing =
    view !== null &&
    view.allowed &&
    view.items.length > 0 &&
    view.hash !== hidden &&
    (!view.done || (seen?.wasOpen === true && doneGone !== view.hash));
  // The card is built once per view: usePresence takes a new object for a new card, and a fresh one on every render
  // would make it set its state on every render (React #301, a white page).
  const shownCard = useMemo(() => (showing && view ? cardOf(view, picked) : null), [showing, view, picked]);
  const card = usePresence(shownCard, EXIT_MS);
  if (!card.value) return null;

  const act = async (kind: TAction) => {
    if (kind === ACTION.safe) {
      onOpenSafe();
      return;
    }
    if (!machine || !card.value) return;
    const line = card.value.line;
    if (kind === ACTION.copy) {
      try {
        await navigator.clipboard.writeText(line.command);
        toast(t('next.setup.copied'));
      } catch {
        toast(line.command);
      }
      return;
    }
    setBusy(true);
    if (kind === ACTION.skip) {
      const res = await askBaseSkip(machine, line.baseId, true);
      setBusy(false);
      if ('ok' in res) toast(t('next.setup.skipped', { name: line.baseName ?? line.baseId }));
      else toast(t('next.setup.askFailed'), { tone: 'error' });
      setNudge((n) => n + 1);
      return;
    }
    // «Повторить» and «Запустить» go to both: the base package and the hub's list (where there is one).
    const [base, list] = await Promise.all([
      askBaseRun(machine),
      view?.items.some((it) => !it.base) ? askSetupRun(machine) : null,
    ]);
    setBusy(false);
    if (!('ok' in base) && (list === null || !('ok' in list)))
      toast(t('next.setup.askFailed'), { tone: 'error' });
    setNudge((n) => n + 1);
  };
  const sendProxy = async (url: string): Promise<boolean> => {
    if (!machine) return false;
    const res = await askBaseProxy(machine, url);
    if ('ok' in res) {
      setProxy(res.value.proxy);
      toast(t('next.setup.proxy.saved'));
      setNudge((n) => n + 1);
      return true;
    }
    toast(t('next.setup.proxy.bad', { why: t('next.setup.proxy.hint') }), { tone: 'error' });
    return false;
  };
  return (
    <SetupCard
      card={card.value}
      closing={card.state === 'closing'}
      busy={busy}
      onPick={(i) => setPicked((p) => (p === i ? null : i))}
      onAction={(k) => void act(k)}
      onProxy={sendProxy}
      proxy={proxy}
      onHide={() => {
        if (!view) return;
        writeHidden(view.hash);
        setHidden(view.hash);
      }}
    />
  );
};
