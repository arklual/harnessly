// The smart notice on the stores: what waits for the person comes from the notices (a question, a permission or a plan
// in a chat they are not looking at) and from the machine's one-time codes; a finished or broken turn shows for a few
// seconds. The open card reads its question or permission from the chat's channel, held only while the card is open,
// and answers through the same commands as the chat's own cards; a code goes through the safe as in the chat's column.
// ⌘J opens and folds it (with nothing waiting it opens the notification centre); Esc folds it and gives the field back
// its focus. What was done from it goes to the history the notification centre lists. A machine in the red (its
// health, as the machine's banner has it) is an item too: what weighs on it comes from the Machine tool's figures, read
// while its card is open, and a program can be stopped from there. Swept aside or put away, an item is hidden until its
// next event; a toast gives it back.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useCodeAsks } from '@/next/bindings/code-asks';
import { firstToPing, soundOn } from '@/next/bindings/notices';
import { settled } from '@/next/bindings/outcome';
import { answerQuestion, decidePermission } from '@/next/commands';
import { useConversationChannel } from '@/next/core/realtime/hub';
import { useKeys } from '@/next/features/keys/use-keys';
import { KEY_SCOPE } from '@/next/features/keys/commands';
import type { IMachineNotice } from '@/next/features/notifications/from-machines';
import { playSound } from '@/next/features/notifications/ping';
import { loadMachineStats, procKey, stopProcess } from '@/next/features/pane/machine/api';
import { cancelCode, isSafeRefusal, putCode, type ICodeAsk } from '@/next/features/safe/api';
import SmartNotice, {
  SMART_CODE,
  SMART_KIND,
  type ISmartHeavy,
  type ISmartItem,
  type TSmartCode,
  type TSmartDetail,
  type TSmartKind,
} from '@/next/features/smart-notice/smart-notice';
import { plural, t } from '@/next/i18n';
import type { Conversation, MachineId, MachineProcKind, MachineStats } from '@/next/protocol/v2';
import { openGates, useGates } from '@/next/stores/conversation';
import { useConversationList } from '@/next/stores/conversations';
import { addHistory, setHistoryPerson } from '@/next/stores/notice-history';
import { SAMPLE_CHAT, setNotify, useNotify, type INotice } from '@/next/stores/notices';
import { toast } from '@/next/ui/toast';

/** A finished or broken turn, and the newest of what waits, tell what came this long in two lines. */
export const ANNOUNCE_MS = 4000;
/** How far over the field of the chat the plate stands, px. */
const OVER_FIELD_PX = 12;
const TICK_MS = 1000;

/** The order on the stack: what waits now first (a code by its deadline), then what waits for an answer (the longest
 *  waiting first), then what broke, then what is ready (the newest first). */
const RANK: Record<TSmartKind, number> = {
  code: 0,
  permission: 1,
  machine: 2,
  question: 2,
  plan: 2,
  failed: 3,
  limit: 3,
  done: 4,
};

export const orderItems = (items: readonly ISmartItem[]): ISmartItem[] =>
  [...items].sort((a, b) => {
    const rank = RANK[a.kind] - RANK[b.kind];
    if (rank !== 0) return rank;
    if (a.kind === SMART_KIND.code) return (a.until ?? 0) - (b.until ?? 0);
    if (RANK[a.kind] <= RANK[SMART_KIND.question]) return a.at - b.at;
    return b.at - a.at;
  });

const GATE_KIND: Record<NonNullable<Conversation['gate']>['kind'], TSmartKind> = {
  question: SMART_KIND.question,
  permission: SMART_KIND.permission,
  plan: SMART_KIND.plan,
};

const NOTICE_KIND_OF: Record<Exclude<INotice['kind'], 'waiting'>, TSmartKind> = {
  done: SMART_KIND.done,
  failed: SMART_KIND.failed,
  limit: SMART_KIND.limit,
};

/** The notices as items: what waits while its chat's gate is open, the rest while it is news (or the card is open). */
export const itemsOfNotices = (
  notices: readonly INotice[],
  convs: ReadonlyMap<string, Conversation>,
  now: number,
  open: boolean,
): ISmartItem[] =>
  notices.flatMap((n): ISmartItem[] => {
    const base = {
      key: n.key,
      chatId: n.chatId,
      icon: n.icon,
      title: n.title,
      text: n.text,
      at: n.at,
      until: null,
    };
    if (n.chatId === SAMPLE_CHAT)
      return now - n.at < ANNOUNCE_MS ? [{ ...base, kind: SMART_KIND.done, chatId: null }] : [];
    if (n.kind === 'waiting') {
      const gate = convs.get(n.chatId)?.gate;
      return gate ? [{ ...base, kind: GATE_KIND[gate.kind] }] : [];
    }
    return open || now - n.at < ANNOUNCE_MS ? [{ ...base, kind: NOTICE_KIND_OF[n.kind] }] : [];
  });

/** The machine's codes as items; `skew` is the machine's clock minus this page's. */
export const itemsOfCodes = (asks: readonly ICodeAsk[], skew: number): ISmartItem[] =>
  asks.map((ask) => ({
    key: `code:${ask.id}`,
    kind: SMART_KIND.code,
    chatId: ask.conv || null,
    icon: 'key-round',
    title: ask.title,
    text: t('next.smart.codeWaits'),
    at: ask.at - skew,
    until: ask.until - skew,
  }));

/** The machines in the red as items, in the words of the plate. */
export const itemsOfMachines = (notices: readonly IMachineNotice[]): ISmartItem[] =>
  notices.map((n) => ({
    key: `machine:${n.alert.key}`,
    kind: SMART_KIND.machine,
    chatId: null,
    icon: n.alert.icon,
    title: n.name,
    text: t('next.smart.machine.short'),
    at: n.since,
    until: null,
  }));

type TMetric = IMachineNotice['alert']['metric'];

const PROC_ICON: Record<MachineProcKind, string> = {
  service: 'server',
  agent: 'messages-square',
  dev: 'square-terminal',
  other: 'app-window',
};

/** How full the machine is in what runs short, 0…1. */
const LEVEL: Record<TMetric, (s: MachineStats) => number> = {
  mem: (s) => (s.mem.total > 0 ? s.mem.used / s.mem.total : 1),
  cpu: (s) => s.cpu,
  disk: (s) => (s.disk.total > 0 ? 1 - s.disk.free / s.disk.total : 1),
};

/** How much a program weighs in what runs short; the disk is not of programs. */
const WEIGHT: Record<TMetric, (p: MachineStats['procs'][number]) => number> = {
  mem: (p) => p.rss,
  cpu: (p) => p.cpu,
  disk: () => 0,
};

const HEAVY_ROWS = 3;

/** A machine's card: what runs short in words, how full it is, and what weighs most — the agents together, then the
 *  heaviest programs one by one. Without the figures yet, the words alone. */
export const machineDetail = (metric: TMetric, stats: MachineStats | null): TSmartDetail => {
  const text = t(`next.smart.machine.${metric}`);
  if (!stats) return { k: 'machine', text, level: 1, heavy: [] };
  const level = Math.max(0, Math.min(1, LEVEL[metric](stats)));
  if (metric === 'disk') return { k: 'machine', text, level, heavy: [] };
  const weight = WEIGHT[metric];
  const agents = stats.procs.filter((p) => p.kind === 'agent');
  const rows: { weight: number; heavy: ISmartHeavy }[] = stats.procs
    .filter((p) => p.kind !== 'agent')
    .map((p) => ({
      weight: weight(p),
      heavy: {
        key: procKey(p),
        icon: PROC_ICON[p.kind],
        name: p.name,
        text: t(`next.smart.machine.kind.${p.kind}`),
        stoppable: !p.guard && p.kind !== 'service',
      },
    }));
  if (agents.length > 0)
    rows.push({
      weight: agents.reduce((sum, p) => sum + weight(p), 0),
      heavy: {
        key: 'agents',
        icon: PROC_ICON.agent,
        name: t('next.smart.machine.agents'),
        text: plural(agents.length, 'next.smart.machine.agentsBusy'),
        stoppable: false,
      },
    });
  rows.sort((a, b) => b.weight - a.weight);
  return { k: 'machine', text, level, heavy: rows.slice(0, HEAVY_ROWS).map((r) => r.heavy) };
};

const STATS_MS = 10_000;

/** The Machine tool's figures of one machine, read again every few seconds while asked for. */
const useMachineStats = (machine: MachineId | null): MachineStats | null => {
  const [stats, setStats] = useState<{ machine: MachineId; stats: MachineStats } | null>(null);
  useEffect(() => {
    if (!machine) return;
    let live = true;
    const load = () =>
      void loadMachineStats(machine).then((res) => {
        if (live && 'ok' in res) setStats({ machine, stats: res.value });
      });
    load();
    const timer = setInterval(load, STATS_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [machine]);
  return stats?.machine === machine ? stats.stats : null;
};

/** A clock for the plate: ticks while something is on it, so a code's ring runs and an announcement ends. */
const useTick = (on: boolean): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    const timer = setInterval(() => setNow(Date.now()), TICK_MS);
    return () => clearInterval(timer);
  }, [on]);
  return now;
};

const FIELD = '.hx-composer';

/** The middle of the chat's field on screen and how far its top is from the window's bottom; null without a field. */
const useFieldAnchor = (on: boolean, key: string): { x: number; bottom: number } | null => {
  const [anchor, setAnchor] = useState<{ x: number; bottom: number } | null>(null);
  useLayoutEffect(() => {
    if (!on) return;
    const measure = () => {
      const field = document.querySelector<HTMLElement>(FIELD);
      const box = field?.getBoundingClientRect();
      const next =
        box && box.width > 0
          ? {
              x: Math.round(box.left + box.width / 2),
              bottom: Math.round(window.innerHeight - box.top + OVER_FIELD_PX),
            }
          : null;
      setAnchor((was) => (was?.x === next?.x && was?.bottom === next?.bottom ? was : next));
    };
    const first = requestAnimationFrame(measure);
    // The field grows with its text and moves with the panes: a cheap look every half second keeps up with all of it.
    const timer = setInterval(measure, 500);
    window.addEventListener('resize', measure);
    return () => {
      cancelAnimationFrame(first);
      clearInterval(timer);
      window.removeEventListener('resize', measure);
    };
  }, [on, key]);
  return on ? anchor : null;
};

const focusField = () => {
  document.querySelector<HTMLElement>(`${FIELD} textarea, ${FIELD} [contenteditable="true"]`)?.focus();
};

export interface ISmartNoticeHostProps {
  person: string;
  notices: readonly INotice[];
  /** The machines in the red. */
  machineNotices: readonly IMachineNotice[];
  /** The active machine: its one-time codes. */
  machine: MachineId | null;
  /** The chat on screen. */
  currentId: string | null;
  onOpenChat: (id: string) => void;
  /** Opens the notification centre: ⌘J with nothing waiting. */
  onOpenCenter: () => void;
  /** Opens a machine's card with its figures. */
  onOpenMachine: (id: MachineId) => void;
  onOpenSettings: () => void;
}

const SmartNoticeHost = ({
  person,
  notices,
  machineNotices,
  machine,
  currentId,
  onOpenChat,
  onOpenCenter,
  onOpenMachine,
  onOpenSettings,
}: ISmartNoticeHostProps) => {
  setHistoryPerson(person);
  const settings = useNotify();
  const convs = useConversationList(null);
  const codes = useCodeAsks(machine);
  const [open, setOpen] = useState(false);
  const [mainKey, setMainKey] = useState<string | null>(null);
  const [byKeys, setByKeys] = useState(false);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const now = useTick(notices.length > 0 || codes.asks.length > 0 || machineNotices.length > 0);

  const byId = useMemo(() => new Map<string, Conversation>((convs ?? []).map((c) => [c.id, c])), [convs]);
  const items = useMemo(
    () =>
      settings.cards
        ? orderItems([
            ...itemsOfCodes(codes.asks, codes.skew),
            ...itemsOfMachines(machineNotices),
            ...itemsOfNotices(notices, byId, now, open),
          ]).filter((item) => !hidden.has(item.key))
        : [],
    [settings.cards, codes.asks, codes.skew, machineNotices, notices, byId, now, open, hidden],
  );
  const main = items.find((item) => item.key === mainKey) ?? items[0] ?? null;
  const conv = main?.chatId ? (byId.get(main.chatId) ?? null) : null;
  const sick =
    main?.kind === SMART_KIND.machine
      ? (machineNotices.find((n) => main.key === `machine:${n.alert.key}`) ?? null)
      : null;
  const stats = useMachineStats(open && sick ? sick.id : null);

  // Nothing left: the card folds, so the next one comes as a line (adjusted while rendering, as React advises).
  const empty = items.length === 0;
  const [wasEmpty, setWasEmpty] = useState(empty);
  if (empty !== wasEmpty) {
    setWasEmpty(empty);
    if (empty) setOpen(false);
  }

  // A new code is a sound of «waits now» (a chat's question or permission sounds with its notice).
  const heard = useRef<ReadonlySet<string> | null>(null);
  useEffect(() => {
    const keys = new Set(codes.asks.map((ask) => ask.id));
    const before = heard.current;
    heard.current = keys;
    if (before === null || document.visibilityState !== 'visible') return;
    const fresh = codes.asks.find((ask) => !before.has(ask.id));
    if (fresh && soundOn(settings, 'urgent') && firstToPing(`code:${fresh.id}`)) playSound('urgent');
  }, [codes.asks, settings]);

  // A machine going into the red sounds as urgent, once.
  const sickHeard = useRef<ReadonlySet<string> | null>(null);
  useEffect(() => {
    const keys = new Set(machineNotices.map((n) => n.alert.key));
    const before = sickHeard.current;
    sickHeard.current = keys;
    if (before === null || document.visibilityState !== 'visible' || !settings.cards) return;
    const fresh = machineNotices.find((n) => !before.has(n.alert.key));
    if (fresh && soundOn(settings, 'urgent') && firstToPing(`machine:${fresh.alert.key}`))
      playSound('urgent');
  }, [machineNotices, settings]);

  // The open card's question or permission: the chat's channel, held while the card shows it.
  const needsGate =
    open && conv !== null && (main?.kind === SMART_KIND.question || main?.kind === SMART_KIND.permission);
  useConversationChannel(needsGate ? conv.machineId : null, needsGate ? conv.id : null);
  const gates = useGates(needsGate ? conv.id : null);
  const gate = useMemo(() => openGates(gates), [gates]);
  const question = gate.questions[0] ?? null;
  const permission = gate.permissions[0] ?? null;

  const detail = useMemo((): TSmartDetail => {
    if (!main) return { k: 'plain' };
    if (main.kind === SMART_KIND.code) {
      const ask = codes.asks.find((a) => `code:${a.id}` === main.key);
      return ask ? { k: 'code', why: ask.why, digits: ask.digits } : { k: 'plain' };
    }
    if (main.kind === SMART_KIND.question) {
      if (!question) return { k: 'loading' };
      const [only, ...more] = question.items;
      // Several questions or a choice of several are answered in the chat, where they have room.
      if (!only || more.length > 0 || only.multi) return { k: 'plain' };
      return { k: 'question', text: only.question, options: only.options.map((o) => o.label) };
    }
    if (main.kind === SMART_KIND.permission) {
      if (!permission) return { k: 'loading' };
      if (permission.kind !== 'tool') return { k: 'plain' };
      return { k: 'permission', command: permission.command ?? permission.tool ?? '' };
    }
    if (sick) return machineDetail(sick.alert.metric, stats);
    return { k: 'plain' };
  }, [main, codes.asks, question, permission, sick, stats]);

  const anchor = useFieldAnchor(settings.place === 'bottom-center' && items.length > 0, currentId ?? '');

  const remember = useCallback(
    (item: ISmartItem, text: string, icon: string) =>
      addHistory({
        key: `${item.key}:done`,
        chatId: item.chatId,
        icon,
        title: item.title,
        text,
        at: Date.now(),
      }),
    [],
  );

  const onAnswer = async (item: ISmartItem, answer: { choice: number } | { text: string }) => {
    if (!conv || !question) return;
    const only = question.items[0];
    const said = 'text' in answer ? answer.text : (only?.options[answer.choice]?.label ?? '');
    await settled(
      answerQuestion(
        conv.machineId,
        conv.id,
        question.id,
        'text' in answer ? [{ i: 0, choices: [], text: answer.text }] : [{ i: 0, choices: [answer.choice] }],
      ),
    );
    remember(item, t('next.smart.history.answer', { text: said }), 'check');
  };

  const onDecide = async (item: ISmartItem, allow: boolean) => {
    if (!conv || !permission) return;
    const option = permission.options.find((o) => o.scope === (allow ? 'once' : 'deny'));
    if (!option) return;
    await settled(decidePermission(conv.machineId, conv.id, permission.id, option.id));
    remember(
      item,
      t(allow ? 'next.smart.history.allowed' : 'next.smart.history.denied'),
      allow ? 'check' : 'ban',
    );
  };

  const askOf = (item: ISmartItem) => codes.asks.find((a) => `code:${a.id}` === item.key) ?? null;

  const onCode = async (item: ISmartItem, code: string): Promise<TSmartCode> => {
    const ask = askOf(item);
    if (!machine || !ask) return SMART_CODE.gone;
    const res = await putCode(machine, ask.id, code);
    if (!('ok' in res)) return SMART_CODE.failed;
    if (!isSafeRefusal(res.value)) {
      codes.drop(ask.id);
      remember(item, t('next.smart.history.code'), 'key-round');
      return SMART_CODE.ok;
    }
    if (res.value.refused === 'not-found') return SMART_CODE.gone;
    return res.value.refused === 'bad-code' ? SMART_CODE.bad : SMART_CODE.failed;
  };

  const onCancelCode = async (item: ISmartItem) => {
    const ask = askOf(item);
    if (!machine || !ask) return;
    await cancelCode(machine, ask.id);
    codes.drop(ask.id);
  };

  const onStop = async (item: ISmartItem, heavyKey: string) => {
    const proc = stats?.procs.find((p) => procKey(p) === heavyKey);
    if (!sick || !proc) return;
    try {
      await stopProcess(sick.id, proc);
    } catch (error) {
      toast(t('next.smart.machine.stopFailed'), {
        tone: 'error',
        description: error instanceof Error ? error.message : undefined,
      });
      throw error;
    }
    remember(item, t('next.smart.history.stopped', { name: proc.name }), 'circle-stop');
  };

  const onHide = (item: ISmartItem) => {
    setHidden((was) => new Set(was).add(item.key));
    toast(t('next.smart.hidden'), {
      description: t(item.kind === SMART_KIND.machine ? 'next.smart.hiddenMachine' : 'next.smart.hiddenText'),
      action: {
        label: t('next.smart.unhide'),
        onClick: () =>
          setHidden((was) => {
            const next = new Set(was);
            next.delete(item.key);
            return next;
          }),
      },
    });
  };

  useKeys(KEY_SCOPE.global, {
    'global.notice': () => {
      if (items.length === 0) {
        onOpenCenter();
        return;
      }
      if (open) {
        setOpen(false);
        focusField();
        return;
      }
      setByKeys(true);
      setOpen(true);
    },
  });

  return (
    <SmartNotice
      items={items}
      mainKey={main?.key ?? null}
      detail={detail}
      open={open && items.length > 0}
      place={settings.place}
      anchor={anchor}
      now={now}
      onOpen={(next) => {
        if (next) setByKeys(false);
        setOpen(next);
      }}
      onMain={setMainKey}
      onOpenChat={(item) => {
        setOpen(false);
        if (item.chatId) onOpenChat(item.chatId);
      }}
      onAnswer={onAnswer}
      onDecide={onDecide}
      onCode={onCode}
      onCancelCode={onCancelCode}
      onStop={onStop}
      onOpenMachine={(item) => {
        setOpen(false);
        const of = machineNotices.find((n) => item.key === `machine:${n.alert.key}`);
        if (of) onOpenMachine(of.id);
      }}
      onHide={onHide}
      onMute={(sound) => {
        setNotify({ sounds: { [sound]: false } });
        toast(t('next.smart.muted'), { description: t(`next.notices.settings.sound.${sound}`) });
      }}
      onSettings={() => {
        setOpen(false);
        onOpenSettings();
      }}
      announceMs={ANNOUNCE_MS}
      returnFocus={focusField}
      focusOnOpen={byKeys}
    />
  );
};

export default SmartNoticeHost;
