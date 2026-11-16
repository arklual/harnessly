// The widgets under the chat's right column (features/widgets): the person's list (stores/side-widgets.ts, the same on
// every computer), harnessly's own widgets drawn from the page's own stores, and the apps' widgets — a view of an app
// with the `side` target in its sandboxed frame, given only the host's data its manifest names and, for a smart one, the
// model's short answer. A widget an agent made (`harnessly widget create`) joins the stack once by itself.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { loadApps } from '@/next/features/apps/api';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps, type IAppView, type IInstalledApp } from '@/next/features/app-views/api';
import { SlotBoundary } from '@/next/features/app-views/ui-slot';
import { useMachineLimits } from '@/next/bindings/use-limits';
import {
  leftOf,
  levelOf,
  profileName,
  windowShort,
  windowsOf,
  type MachineLimits,
  type ProviderLimits,
} from '@/next/features/limits/model';
import {
  BUILTIN_WIDGETS,
  canAdd,
  MAX_NOTE,
  WIDGET_KIND,
  clockText,
  shownWidgets,
  smartEveryMs,
  timerLeft,
  withAdded,
  withFolded,
  withMoved,
  withNewApps,
  withNote,
  withRemoved,
  withTimer,
  type ISideWidget,
  type TBuiltinWidget,
} from '@/next/features/widgets/model';
import SideWidgets, { type IGalleryEntry, type IWidgetLook } from '@/next/features/widgets/side-widgets';
import { useSmart, type TSmartState } from '@/next/features/widgets/smart';
import { t } from '@/next/i18n';
import type { AppFeed, Conversation, MachineId, MachineLoad } from '@/next/protocol/v2';
import { useConversationList } from '@/next/stores/conversations';
import { useMachine, useMachineLoad } from '@/next/stores/machines';
import { updateWidgets, useWidgets } from '@/next/stores/side-widgets';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';

const BUILTIN_ICON = {
  limits: 'gauge',
  machine: 'server',
  subagents: 'workflow',
  pipeline: 'git-merge',
  note: 'pencil',
  clock: 'clock',
} as const satisfies Record<TBuiltinWidget, string>;

const isBuiltin = (kind: string): kind is TBuiltinWidget =>
  (BUILTIN_WIDGETS as readonly string[]).includes(kind);

/** The app's views a side widget can be. */
const sideViews = (apps: readonly IInstalledApp[]): { app: IInstalledApp; view: IAppView }[] =>
  apps.flatMap((app) => app.views.filter((v) => v.targets.includes('side')).map((view) => ({ app, view })));

// ── harnessly's own widgets ──

const Meter = ({ left }: { left: number }) => (
  <svg className="hx-wgm" viewBox="0 0 100 4" preserveAspectRatio="none" aria-hidden="true">
    <rect className="hx-wgm__track" x="0" y="0" width="100" height="4" rx="2" />
    <rect className="hx-wgm__fill" data-level={levelOf(left)} x="0" y="0" width={left} height="4" rx="2" />
  </svg>
);

const limitRows = (limits: MachineLimits): { key: string; name: string; limits: ProviderLimits }[] => {
  const rows: { key: string; name: string; limits: ProviderLimits }[] = [];
  if (limits.claude) rows.push({ key: 'claude', name: 'Claude', limits: limits.claude });
  if (limits.codex) rows.push({ key: 'codex', name: 'Codex', limits: limits.codex });
  for (const [key, p] of Object.entries(limits.profiles))
    rows.push({
      key,
      name: p.account ? `${profileName(p.profile)} · ${p.account}` : profileName(p.profile),
      limits: p,
    });
  return rows.filter((r) => windowsOf(r.limits).length > 0);
};

const LimitsBody = ({ machine }: { machine: MachineId }) => {
  const limits = useMachineLimits(machine);
  if (!limits) return <div className="hx-wg__wait" />;
  const rows = limitRows(limits);
  if (rows.length === 0) return <p className="hx-wg__quiet">{t('next.widgets.limits.none')}</p>;
  return (
    <div className="hx-wgl">
      {rows.map((r) => (
        <div key={r.key} className="hx-wgl__row">
          <span className="hx-wgl__name">{r.name}</span>
          {windowsOf(r.limits)
            .filter((w) => w.kind !== 'month')
            .map((w) => {
              const left = leftOf(w.window);
              return (
                <span key={w.kind} className="hx-wgl__win">
                  <small>{windowShort(w.kind)}</small>
                  <Meter left={left} />
                  <b>{left}%</b>
                </span>
              );
            })}
        </div>
      ))}
    </div>
  );
};

const pct = (part: number, whole: number) => (whole > 0 ? Math.round((part / whole) * 100) : 0);

const MachineBody = ({ machine }: { machine: MachineId }) => {
  const load = useMachineLoad(machine);
  const name = useMachine(machine)?.machine.name ?? '';
  if (!load) return <div className="hx-wg__wait" />;
  const rows = [
    { key: 'cpu', label: t('next.widgets.machine.cpu'), used: Math.round(load.cpu * 100) },
    { key: 'mem', label: t('next.widgets.machine.mem'), used: pct(load.memUsed, load.memTotal) },
    {
      key: 'disk',
      label: t('next.widgets.machine.disk'),
      used: pct(load.diskTotal - load.diskFree, load.diskTotal),
    },
  ];
  return (
    <div className="hx-wgl">
      {name && <span className="hx-wgl__name">{name}</span>}
      {rows.map((r) => (
        <span key={r.key} className="hx-wgl__win">
          <small>{r.label}</small>
          <Meter left={100 - r.used} />
          <b>{r.used}%</b>
        </span>
      ))}
    </div>
  );
};

const ACTIVE: ReadonlySet<string> = new Set([
  'starting',
  'working',
  'waiting-question',
  'waiting-permission',
]);
const SUBS_SHOWN = 5;

const activeSubchats = (list: readonly Conversation[] | null): Conversation[] =>
  (list ?? []).filter((c) => c.parentId !== undefined && ACTIVE.has(c.state.k)).slice(0, SUBS_SHOWN);

const SubagentsBody = ({ onOpenChat }: { onOpenChat: (id: string) => void }) => {
  const subs = activeSubchats(useConversationList(null));
  if (subs.length === 0) return <p className="hx-wg__quiet">{t('next.widgets.subagents.none')}</p>;
  return (
    <div className="hx-wgr">
      {subs.map((c) => (
        <button key={c.id} type="button" className="hx-wgr__row" onClick={() => onOpenChat(c.id)}>
          <span className="hx-wgr__dot" data-state={c.state.k.startsWith('waiting') ? 'wait' : 'run'} />
          <span className="hx-wgr__title">{c.title || t('next.widgets.subagents.untitled')}</span>
        </button>
      ))}
    </div>
  );
};

const PIPELINE_POLL_MS = 120_000;
const MRS_SHOWN = 4;

type TPipelineRead = { k: 'wait' } | { k: 'off' } | { k: 'ready'; feed: AppFeed };

const PipelineBody = ({ machine }: { machine: MachineId }) => {
  const [read, setRead] = useState<TPipelineRead>({ k: 'wait' });
  useEffect(() => {
    let live = true;
    let timer = 0;
    const go = async () => {
      if (!document.hidden) {
        const res = await loadApps(machine, ['gitlab']);
        if (!live) return;
        const feed = 'ok' in res ? res.value.apps.find((a) => a.id === 'gitlab') : undefined;
        setRead(feed && feed.state === 'ready' ? { k: 'ready', feed } : { k: 'off' });
      }
      if (live) timer = window.setTimeout(() => void go(), PIPELINE_POLL_MS);
    };
    void go();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [machine]);
  if (read.k === 'wait') return <div className="hx-wg__wait" />;
  if (read.k === 'off') return <p className="hx-wg__quiet">{t('next.widgets.pipeline.off')}</p>;
  const mine = read.feed.sections
    .flatMap((s) => s.rows)
    .filter((r) => r.mr !== null && r.me.includes('author') && r.closedAt === null)
    .slice(0, MRS_SHOWN);
  if (mine.length === 0) return <p className="hx-wg__quiet">{t('next.widgets.pipeline.none')}</p>;
  return (
    <div className="hx-wgr">
      {mine.map((r) => (
        <a key={r.key} className="hx-wgr__row" href={r.url} target="_blank" rel="noreferrer">
          <span className="hx-wgr__dot" data-state={r.mr?.pipeline ?? 'none'} />
          <span className="hx-wgr__title">{r.title}</span>
          <span className="hx-wgr__id">{r.id}</span>
        </a>
      ))}
    </div>
  );
};

const NoteBody = ({ widget }: { widget: ISideWidget }) => {
  const [text, setText] = useState(widget.note ?? '');
  const [had, setHad] = useState(widget.note ?? '');
  // Another computer changed it: the new text comes in while this one is not being typed into.
  if ((widget.note ?? '') !== had) {
    setHad(widget.note ?? '');
    setText(widget.note ?? '');
  }
  const save = () => {
    if (text !== (widget.note ?? '')) updateWidgets((now) => withNote(now, widget.id, text));
  };
  return (
    <textarea
      className="hx-wgn"
      value={text}
      maxLength={MAX_NOTE}
      placeholder={t('next.widgets.note.placeholder')}
      aria-label={t('next.widgets.note.title')}
      onChange={(e) => setText(e.target.value)}
      onBlur={save}
    />
  );
};

const TIMERS = [5, 15, 25] as const;

const ClockBody = ({ widget }: { widget: ISideWidget }) => {
  const endsAt = widget.endsAt ?? null;
  const [now, setNow] = useState(() => Date.now());
  const running = endsAt !== null && endsAt > now;
  useEffect(() => {
    // A second while a timer runs; the minute otherwise.
    const timer = window.setInterval(() => setNow(Date.now()), running ? 1000 : 15_000);
    return () => window.clearInterval(timer);
  }, [running]);
  // The end is told once, on the computer that saw it come (not one opened long after).
  const rang = useRef<number | null>(null);
  useEffect(() => {
    if (endsAt === null || running || rang.current === endsAt || now - endsAt > 60_000) return;
    rang.current = endsAt;
    toast(t('next.widgets.clock.done'));
  }, [endsAt, running, now]);
  const time = new Date(now).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return (
    <div className="hx-wgc">
      <span className="hx-wgc__big">
        {running && endsAt !== null ? clockText(timerLeft(endsAt, now)) : time}
      </span>
      <span className="hx-wgc__row">
        {running ? (
          <button
            type="button"
            className="hx-wgc__btn"
            onClick={() => updateWidgets((s) => withTimer(s, widget.id, null))}
          >
            <Icon name="square" size="sm" />
            {t('next.widgets.clock.stop')}
          </button>
        ) : (
          TIMERS.map((m) => (
            <button
              key={m}
              type="button"
              className="hx-wgc__btn"
              onClick={() => updateWidgets((s) => withTimer(s, widget.id, Date.now() + m * 60_000))}
            >
              {t('next.widgets.clock.minutes', { n: m })}
            </button>
          ))
        )}
      </span>
    </div>
  );
};

// ── the apps' widgets ──

/** The host's data an app widget named, as compact as it is useful: what the model and the view may read. */
const useSources = (machine: MachineId, sources: readonly string[]) => {
  const limits = useMachineLimits(sources.includes('limits') ? machine : null);
  const load = useMachineLoad(sources.includes('machine') ? machine : null);
  const convs = useConversationList(null);
  const wantSubs = sources.includes('subagents');
  return useMemo(() => {
    const out: Record<string, unknown> = {};
    if (sources.includes('limits') && limits)
      out.limits = limitRows(limits).map((r) => ({
        name: r.name,
        windows: windowsOf(r.limits).map((w) => ({
          kind: w.kind,
          leftPct: leftOf(w.window),
          resetsAt: w.window.resetsAt,
        })),
      }));
    if (sources.includes('machine') && load) out.machine = machineSummary(load);
    if (wantSubs) out.subagents = activeSubchats(convs).map((c) => ({ title: c.title, state: c.state.k }));
    return out;
  }, [sources, limits, load, convs, wantSubs]);
};

const machineSummary = (load: MachineLoad) => ({
  cpuPct: Math.round(load.cpu * 100),
  memPct: pct(load.memUsed, load.memTotal),
  diskFreeGb: Math.round(load.diskFree / 1e9),
});

interface IAppWidgetProps {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  viewPort: number;
  smart: TSmartState | null;
  data: Record<string, unknown>;
}

const AppWidgetBody = ({ machine, app, view, viewPort, smart, data }: IAppWidgetProps) => {
  const [reload, setReload] = useState(0);
  const full = useMemo(
    () => ({
      sources: data,
      ...(smart
        ? {
            smart: {
              text: smart.answer?.text ?? null,
              at: smart.answer?.at ?? null,
              asking: smart.k === 'asking',
            },
          }
        : {}),
    }),
    [data, smart],
  );
  return (
    <div className="hx-wga" data-size={view.widget?.size ?? 's'}>
      <SlotBoundary onRetry={() => setReload((n) => n + 1)}>
        <AppFrame
          machine={machine}
          app={app}
          view={view}
          target="side"
          viewPort={viewPort}
          conversation={null}
          data={full}
          reloadToken={reload}
        />
      </SlotBoundary>
    </div>
  );
};

/** The latest value, for a timer or an ask that reads it when it goes. */
const useLatest = <T,>(value: T) => {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
};

/** The «ask now» of the smart widgets on screen, by widget: the card's header button calls it. */
const smartRefresh = new Map<string, () => void>();

/** One app widget: its sources, its refresh, its smart answer; drawn only while the card is (side-widgets.tsx). */
const AppWidget = ({
  machine,
  app,
  view,
  viewPort,
  widget,
}: {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  viewPort: number;
  widget: ISideWidget;
}) => {
  const spec = view.widget;
  const sources = useMemo(() => spec?.sources ?? [], [spec]);
  const live = useSources(machine, sources);
  const latest = useLatest(live);
  // The view gets its data when it comes, then again no more often than its refresh asks (never, without one).
  const [given, setGiven] = useState<Record<string, unknown> | null>(null);
  if (given === null && Object.keys(live).length > 0) setGiven(live);
  const refresh = spec?.refresh ?? 0;
  useEffect(() => {
    if (refresh <= 0) return;
    const timer = window.setInterval(() => {
      if (!document.hidden) setGiven(latest.current);
    }, refresh * 1000);
    return () => window.clearInterval(timer);
  }, [refresh, latest]);
  const smart = useSmart(
    machine,
    widget.id,
    spec?.smart?.prompt ?? null,
    smartEveryMs(spec?.smart?.every),
    true,
    () => JSON.stringify(latest.current),
  );
  const ask = smart.refresh;
  useEffect(() => {
    smartRefresh.set(widget.id, ask);
    return () => {
      smartRefresh.delete(widget.id);
    };
  }, [widget.id, ask]);
  return (
    <AppWidgetBody
      machine={machine}
      app={app}
      view={view}
      viewPort={viewPort}
      smart={spec?.smart ? smart.state : null}
      data={given ?? live}
    />
  );
};

// ── the stack ──

export interface ISideWidgetsBindingProps {
  machine: MachineId;
  onOpenChat: (id: string) => void;
}

const SideWidgetsBinding = ({ machine, onOpenChat }: ISideWidgetsBindingProps) => {
  const state = useWidgets();
  const { list } = useInstalledApps(machine);
  const found = useMemo(() => (list ? sideViews(list.apps) : null), [list]);
  // A widget an agent made joins the stack once by itself.
  useEffect(() => {
    if (!found) return;
    updateWidgets((now) =>
      withNewApps(
        now,
        found.map((f) => ({ app: f.app.id, view: f.view.id })),
      ),
    );
  }, [found]);
  const appOf = useCallback(
    (w: ISideWidget) => found?.find((f) => f.app.id === w.app && f.view.id === w.view) ?? null,
    [found],
  );
  const widgets = useMemo(
    () =>
      shownWidgets(
        state,
        found ? (app, view) => found.some((f) => f.app.id === app && f.view.id === view) : null,
      ),
    [state, found],
  );

  const lookOf = useCallback(
    (w: ISideWidget): IWidgetLook => {
      if (isBuiltin(w.kind)) return { title: t(`next.widgets.${w.kind}.title`), icon: BUILTIN_ICON[w.kind] };
      const f = appOf(w);
      return {
        title: f?.view.label ?? f?.view.title ?? f?.app.name ?? '',
        icon: f?.view.icon ?? f?.app.icon ?? 'layout-grid',
      };
    },
    [appOf],
  );

  const renderBody = useCallback(
    (w: ISideWidget) => {
      switch (w.kind) {
        case WIDGET_KIND.limits:
          return <LimitsBody machine={machine} />;
        case WIDGET_KIND.machine:
          return <MachineBody machine={machine} />;
        case WIDGET_KIND.subagents:
          return <SubagentsBody onOpenChat={onOpenChat} />;
        case WIDGET_KIND.pipeline:
          return <PipelineBody machine={machine} />;
        case WIDGET_KIND.note:
          return <NoteBody widget={w} />;
        case WIDGET_KIND.clock:
          return <ClockBody widget={w} />;
        case WIDGET_KIND.app: {
          const f = appOf(w);
          if (!f || !list) return null;
          return (
            <AppWidget machine={machine} app={f.app} view={f.view} viewPort={list.viewPort} widget={w} />
          );
        }
      }
    },
    [machine, onOpenChat, appOf, list],
  );

  const renderTool = useCallback(
    (w: ISideWidget) =>
      w.kind === WIDGET_KIND.app && appOf(w)?.view.widget?.smart ? (
        <button
          type="button"
          className="hx-wg__tool"
          aria-label={t('next.widgets.smart.now')}
          data-tip={t('next.widgets.smart.now')}
          onClick={() => smartRefresh.get(w.id)?.()}
        >
          <Icon name="refresh-cw" size="sm" />
        </button>
      ) : null,
    [appOf],
  );

  const add = (make: () => boolean) => {
    if (!make()) toast(t('next.widgets.full'), { tone: 'error' });
  };
  const gallery: IGalleryEntry[] = [
    ...BUILTIN_WIDGETS.filter((kind) => canAdd(state, kind)).map((kind) => ({
      key: kind,
      title: t(`next.widgets.${kind}.title`),
      hint: t(`next.widgets.${kind}.hint`),
      icon: BUILTIN_ICON[kind],
      onAdd: () => add(() => updateWidgets((s) => withAdded(s, kind))),
    })),
    ...(found ?? [])
      .filter((f) => !state.items.some((x) => x.app === f.app.id && x.view === f.view.id))
      .map((f) => ({
        key: `app:${f.app.id}/${f.view.id}`,
        title: f.view.label ?? f.view.title ?? f.app.name,
        hint: f.app.description,
        icon: f.view.icon ?? f.app.icon ?? 'layout-grid',
        onAdd: () =>
          add(() => updateWidgets((s) => withAdded(s, WIDGET_KIND.app, { app: f.app.id, view: f.view.id }))),
      })),
  ];

  return (
    <SideWidgets
      widgets={widgets}
      lookOf={lookOf}
      renderBody={renderBody}
      renderTool={renderTool}
      gallery={gallery}
      onMove={(id, at, after) => updateWidgets((s) => withMoved(s, id, at, after))}
      onFold={(id, folded) => updateWidgets((s) => withFolded(s, id, folded))}
      onRemove={(id) => updateWidgets((s) => withRemoved(s, id))}
    />
  );
};

export default SideWidgetsBinding;
