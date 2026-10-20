import { t } from '@/lib/i18n';
import { useMemo, useState, type FormEvent, type ReactNode } from 'react';
import { ChevronDown, ExternalLink, MoreHorizontal } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  LAYER_ALIGN,
  TW_MENU_KIND,
  TwButton,
  TwMenuButton,
  cx,
} from '@/components/agents/ui';
import type { IInspectorIslandProps } from '@/components/agents/inspector/island-types';
import { PreviewFrame, type TOpenState } from '@/components/agents/inspector/preview-frame';
import {
  MachineLoadNotice,
  ProcessStopControl,
  useOverloadGate,
  type IStopState,
} from '@/components/agents/machine-load';
import useIsMobile from '@/hooks/use-is-mobile';
import usePoller from '@/hooks/use-poller';
import { currentMachineId } from '@/lib/app-path';
import {
  createLatestGate,
  fetchPreviewPorts,
  openPreview,
  parsePreviewTarget,
  targetAddress,
  targetLabel,
  type IOpenedPreview,
  type IPreviewTarget,
  type IUrlTarget,
  type TPreviewTarget,
  type TSlotTarget,
} from '@/lib/hl-preview-client';
import { readUrlHistory, writeUrlHistory } from '@/lib/hl-preview-history';
import {
  extractLocalUrls,
  mergePortRows,
  pickAutoOpen,
  previewWorkRoots,
  trackSeenSites,
  type IPortRow,
} from '@/lib/hl-preview-chat-urls';
import { relatedPorts, type TPortGroup } from '@/lib/hl-preview/ports-shared';
import { openPaneTab } from '@/lib/hl-pane-store';
import type { IMachineProcess } from '@/types/server/hl-machine-load-scan';
import { readMachineLoad, useMachineLoad } from '@/lib/hl-machine-load-store';
import { autoOpenAllowed, effectiveLevel, processForPort, processUsage } from '@/lib/hl-machine-load-view';
import {
  claimPreviewSlot,
  nextPreviewLoad,
  patchPreview,
  readPreview,
  usePreviewState,
  type TPreviewState,
} from '@/lib/hl-preview-store';
import type { ITimelineEntry } from '@/types/timeline';

const PORTS_POLL = { intervalMs: 2_000, maxBackoffMs: 30_000 } as const;

/** Same breakpoint as the inspector overlay: below it the preview stays in the right panel. */
export const FLOATING_BREAKPOINT = 1100;

const failureMessage = (error: unknown): string =>
  error instanceof Error ? error.message : t('inspector.previewIsland.openFailed');

/** One gate per tab: the right panel and the floating window open previews of the same tab, the newest one wins. */
const gates = new Map<string, ReturnType<typeof createLatestGate>>();
const gateOf = (key: string) => {
  let gate = gates.get(key);
  if (!gate) gates.set(key, (gate = createLatestGate()));
  return gate;
};

/** The person opened something: the island no longer moves the window to another port by itself. */
export const forgetAutoOpen = (key: string) =>
  patchPreview(key, { auto: { ...readPreview(key).auto, opened: null } });

const slotTargetOf = (target: TPreviewTarget): TSlotTarget =>
  target.kind === 'port' ? { kind: 'port', port: target.port } : { kind: 'url', origin: target.origin };

const slotKey = (target: TSlotTarget): string =>
  target.kind === 'port' ? `port:${target.port}` : target.origin;

/** What the overload gate compares: the target asked for and the one the window shows. */
export const targetGateKey = (target: TPreviewTarget): string => slotKey(slotTargetOf(target));

export const shownGateKey = (state: TPreviewState): string | null =>
  state.kind === 'open' || state.kind === 'opening' || state.kind === 'taken' ? slotKey(state.target) : null;

/**
 * The same port, another page: the design library serves every board from one port, and a board asked for by its
 * path has to replace the one on screen. A bare port keeps whatever page is open there.
 */
export const asksForAnotherPage = (to: TPreviewTarget, state: TPreviewState): boolean =>
  state.kind === 'open' &&
  targetGateKey(to) === slotKey(state.target) &&
  to.path !== '/' &&
  to.path !== state.path;

/** The target a window state shows, to open it again. */
export const reopenTarget = (state: { target: TSlotTarget; path: string }): TPreviewTarget => ({
  ...state.target,
  path: state.path,
});

const asPort = (target: IPreviewTarget): TPreviewTarget => ({ kind: 'port', ...target });

export const previewStorage = (): Storage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

/** Neighbour ports the current preview was opened with: a reload serves the same MFE remotes. */
export const openedNeighbours = (state: TOpenState): number[] => state.preview.neighbours;

export const neighboursIn = (rows: readonly IPortRow[] | null, port: number): number[] =>
  rows
    ? relatedPorts(
        rows.filter((row) => row.listening),
        port,
      )
    : [];

/**
 * Opening a preview in the frame or in a new browser tab. One slot for the port; its neighbours are served under the
 * slot's path. The slot is claimed before the first await, so two openings never get the same free slot.
 */
export const usePreviewOpener = (key: string, machineId?: string) => {
  const [tabError, setTabError] = useState<string | null>(null);
  const [openingTab, setOpeningTab] = useState(false);
  const [tabGate] = useState(createLatestGate);

  // `machineId` opens the target on that machine; slots are the person's hosts, so they are leased per page.
  const requestPreview = (target: TPreviewTarget, neighbours: readonly number[]): Promise<IOpenedPreview> =>
    openPreview(
      target,
      claimPreviewSlot(currentMachineId() ?? '', slotTargetOf(target)),
      neighbours,
      machineId,
    );

  // A ticket works once and lives 60 s: reload and «new tab» always ask for a fresh one.
  const open = async (target: TPreviewTarget, neighbours: readonly number[]) => {
    const gate = gateOf(key);
    const id = gate.next();
    setTabError(null);
    patchPreview(key, { state: { kind: 'opening', target: slotTargetOf(target) } });
    const requestedAt = Date.now();
    try {
      const preview = await requestPreview(target, neighbours);
      if (gate.isLatest(id))
        patchPreview(key, {
          state: {
            kind: 'open',
            target: slotTargetOf(target),
            path: target.path,
            preview,
            load: nextPreviewLoad(),
            openedAt: requestedAt,
            entered: false,
          },
        });
    } catch (error) {
      if (gate.isLatest(id)) patchPreview(key, { state: { kind: 'error', message: failureMessage(error) } });
    }
  };

  // Its own gate: a frame opening meanwhile must not drop the tab's result, only a newer tab opening does.
  const openInTab = async (target: TPreviewTarget, neighbours: readonly number[]) => {
    if (openingTab) return;
    const id = tabGate.next();
    const popup = window.open('about:blank', '_blank');
    if (popup) popup.opener = null;
    setOpeningTab(true);
    setTabError(null);
    try {
      const preview = await requestPreview(target, neighbours);
      if (!tabGate.isLatest(id)) popup?.close();
      else if (popup) popup.location.href = preview.enterUrl;
    } catch (error) {
      popup?.close();
      if (tabGate.isLatest(id)) setTabError(failureMessage(error));
    } finally {
      if (tabGate.isLatest(id)) setOpeningTab(false);
    }
  };

  /** Nothing in flight may bring the closed preview back. */
  const close = () => {
    gateOf(key).next();
    patchPreview(key, { state: { kind: 'idle' }, detached: false });
  };

  return { open, openInTab, close, openingTab, tabError };
};

const GROUP_TITLES: Record<TPortGroup, string> = {
  chat: t('inspector.previewIsland.groupChat'),
  agent: t('inspector.previewIsland.groupAgents'),
  other: t('inspector.previewIsland.groupOther'),
};

const rowNote = (row: IPortRow): string => {
  if (!row.listening) return t('inspector.previewIsland.fromChatNotResponding');
  if (row.owner) return `${row.owner.repo} · ${row.owner.task}`;
  return row.command ?? t('inspector.previewIsland.otherUserProcess');
};

interface IPortProcesses extends IStopState {
  top: readonly IMachineProcess[] | undefined;
}

interface IPortRowProps extends IPortProcesses {
  row: IPortRow;
  current: boolean;
  onOpen: (row: IPortRow) => void;
}

const PortRow = ({ row, current, onOpen, top, ...stop }: IPortRowProps) => {
  const proc = processForPort(top, row.port);
  return (
    <li
      className={cx(
        'tw-preview-port',
        current && 'tw-preview-port-current',
        !row.listening && 'tw-preview-port-silent',
      )}
      aria-current={current || undefined}
    >
      <span className="tw-preview-port-number">{row.port}</span>
      <span className="tw-preview-port-note" title={row.cwd ?? undefined}>
        {rowNote(row)}
      </span>
      {row.kind !== 'unknown' && <span className="tw-preview-port-kind">{row.kind}</span>}
      {proc && (
        <span className="tw-preview-port-proc">
          <span
            className="tw-preview-port-usage"
            title={proc.stuck ? t('inspector.previewIsland.stuckHint') : undefined}
          >
            {proc.stuck ? `${t('inspector.previewIsland.notResponding')} · ` : ''}
            {processUsage(proc)}
          </span>
          {proc.stoppable && <ProcessStopControl proc={proc} {...stop} />}
        </span>
      )}
      <TwButton
        type="button"
        size={BUTTON_SIZE.sm}
        variant={BUTTON_VARIANT.ghost}
        aria-label={t('inspector.previewIsland.openPort', { port: row.port })}
        onClick={() => onOpen(row)}
      >
        {t('inspector.previewIsland.open')}
      </TwButton>
    </li>
  );
};

interface IPortListProps extends IPortProcesses {
  rows: IPortRow[];
  currentPort: number | null;
  onOpen: (row: IPortRow) => void;
}

const PortList = ({ rows, currentPort, onOpen, ...processes }: IPortListProps) => {
  const [otherOpen, setOtherOpen] = useState(false);
  const groups = (['chat', 'agent', 'other'] as const)
    .map((group) => ({ group, rows: rows.filter((row) => row.group === group) }))
    .filter(({ rows: inGroup }) => inGroup.length > 0);
  return (
    <div className="tw-preview-ports">
      {groups.map(({ group, rows: inGroup }) => {
        const folded = group === 'other' && !otherOpen;
        return (
          <section key={group} className="tw-preview-group" aria-label={GROUP_TITLES[group]}>
            {group === 'other' ? (
              <button
                type="button"
                className="tw-preview-group-title tw-preview-group-toggle"
                aria-expanded={!folded}
                onClick={() => setOtherOpen(!otherOpen)}
              >
                {GROUP_TITLES[group]} <span className="tw-preview-group-count">{inGroup.length}</span>
                <ChevronDown
                  className={cx('tw-preview-group-chevron', folded && 'tw-preview-group-chevron-folded')}
                />
              </button>
            ) : (
              <h4 className="tw-preview-group-title">{GROUP_TITLES[group]}</h4>
            )}
            {!folded && (
              <ul className="tw-preview-port-list">
                {inGroup.map((row) => (
                  <PortRow
                    key={row.port}
                    row={row}
                    current={row.port === currentPort}
                    onOpen={onOpen}
                    {...processes}
                  />
                ))}
              </ul>
            )}
          </section>
        );
      })}
    </div>
  );
};

type TTakenState = Extract<TPreviewState, { kind: 'taken' }>;

interface IPreviewBodyProps {
  previewKey: string;
  state: TPreviewState;
  onReopenTaken: (state: TTakenState) => void;
  onStale: (state: TOpenState) => void;
  onEntered: (state: TOpenState) => void;
  onReload: (state: TOpenState) => void;
  onOpenUrl: (target: IUrlTarget) => void;
  address?: (path: string) => ReactNode;
  actions?: ReactNode;
  leading?: ReactNode;
  hideHistory?: boolean;
  pickTip?: string;
}

/** The preview itself, wherever it is shown: the browser tab of the right panel or the floating window. */
export const PreviewBody = ({
  previewKey,
  state,
  onReopenTaken,
  onStale,
  onEntered,
  onReload,
  onOpenUrl,
  address,
  actions,
  leading,
  hideHistory,
  pickTip,
}: IPreviewBodyProps) => {
  if (state.kind === 'error')
    return (
      <p className="tw-preview-error" role="alert">
        {state.message}
      </p>
    );
  if (state.kind === 'opening')
    return (
      <div
        className="tw-preview-frame tw-skeleton"
        role="status"
        aria-label={t('inspector.previewIsland.opening', { target: targetLabel(state.target) })}
      />
    );
  if (state.kind === 'taken') {
    return (
      <div className="tw-preview-taken" role="status">
        <span>
          {t('inspector.previewIsland.taken', {
            target:
              state.target.kind === 'port'
                ? t('inspector.previewIsland.port', { port: state.target.port })
                : state.target.origin,
          })}
        </span>
        <TwButton
          type="button"
          size={BUTTON_SIZE.sm}
          variant={BUTTON_VARIANT.primary}
          onClick={() => onReopenTaken(state)}
        >
          {t('inspector.previewIsland.openAgain')}
        </TwButton>
      </div>
    );
  }
  if (state.kind === 'open') {
    return (
      <PreviewFrame
        key={`${state.preview.origin}#${state.load}`}
        previewKey={previewKey}
        state={state}
        onEntered={() => onEntered(state)}
        onStale={() => onStale(state)}
        onReload={() => onReload(state)}
        onOpenUrl={onOpenUrl}
        address={address}
        actions={actions}
        leading={leading}
        hideHistory={hideHistory}
        pickTip={pickTip}
      />
    );
  }
  return null;
};

interface IPreviewIslandProps extends IInspectorIslandProps {
  entries: readonly ITimelineEntry[];
  workPaths: readonly string[];
  /** Working directory of the session: the first place its dev servers run in. */
  cwd: string | null;
  /** The island body is on screen: the port list is polled only then. */
  active: boolean;
  /** Inside the browser tab, whose address field opens ports and sites: no bar of its own. */
  bare?: boolean;
}

/** The dev servers of the conversation: found by themselves, the newest one opens without asking. */
export const Island = ({
  tab,
  expanded,
  entries,
  workPaths,
  cwd,
  active,
  bare = false,
}: IPreviewIslandProps) => {
  const key = tab?.id ?? '';
  // Small island and expanded dialog read one state per tab, so expanding keeps the open preview.
  const [{ input, state, ports, detached }, patch] = usePreviewState(key);
  const setInput = (value: string) => patch({ input: value });
  const setState = (next: TPreviewState) => patch({ state: next });
  const { open: openWith, openInTab, openingTab, tabError } = usePreviewOpener(key);
  const canFloat = !useIsMobile(FLOATING_BREAKPOINT);
  // The floating window shows the frame then; on a narrow screen there is none and the preview stays here.
  const floating = detached && canFloat;

  const [portsError, setPortsError] = useState<string | null>(null);
  const [manual, setManual] = useState(false);
  const loadKey = currentMachineId() ?? '';
  const { load, stopping, stopped, notice, stop } = useMachineLoad(loadKey, { enabled: active });
  const { guard, gate } = useOverloadGate(key, active);

  const urls = useMemo(() => extractLocalUrls(entries), [entries]);
  const rows = useMemo(() => (ports === null ? null : mergePortRows(ports, urls)), [ports, urls]);
  const roots = useMemo(() => previewWorkRoots(workPaths, cwd), [workPaths, cwd]);

  const [history, setHistory] = useState<IUrlTarget[]>(() =>
    readUrlHistory(previewStorage(), currentMachineId() ?? ''),
  );

  const open = (target: TPreviewTarget, known = rows) =>
    openWith(target, target.kind === 'port' ? neighboursIn(known, target.port) : []);
  const openNow = async (target: TPreviewTarget) => {
    forgetAutoOpen(key);
    openPaneTab('browser');
    await open(target);
    // Only an address that opened is worth remembering.
    const { state: after } = readPreview(key);
    if (
      target.kind === 'url' &&
      after.kind === 'open' &&
      after.target.kind === 'url' &&
      after.target.origin === target.origin
    ) {
      setHistory(writeUrlHistory(previewStorage(), currentMachineId() ?? '', target));
    }
  };
  const openByHand = (target: TPreviewTarget) =>
    guard(targetGateKey(target), shownGateKey(readPreview(key).state), () => void openNow(target));

  usePoller(
    active && tab ? `${tab.id}\n${JSON.stringify(roots)}` : null,
    async (signal) => {
      let next;
      try {
        next = await fetchPreviewPorts(roots, signal);
      } catch (error) {
        if (signal.aborted) return;
        setPortsError(failureMessage(error));
        throw error;
      }
      setPortsError(null);
      const machine = readMachineLoad(loadKey);
      // Auto-open waits for the first load answer: an overloaded machine must not get a preview before it is known.
      if (machine.load === null && machine.error === null) {
        patch({ ports: next });
        return;
      }
      const merged = mergePortRows(next, urls);
      const { state: current, auto, seenSites } = readPreview(key);
      const seen = trackSeenSites(seenSites, next);
      const now = Date.now();
      const shown =
        (current.kind === 'open' || current.kind === 'opening') && current.target.kind === 'port'
          ? current.target.port
          : null;
      const decision = pickAutoOpen({
        rows: merged,
        previousPorts: seen.before,
        empty: current.kind === 'idle' || current.kind === 'error',
        firstShow: !auto.shown,
        seenAt: auto.seenAt,
        now,
        autoOpened: auto.opened && auto.opened.port === shown ? auto.opened : null,
      });
      const target = autoOpenAllowed(effectiveLevel(machine)) ? decision.target : null;
      const opened = target ? { port: target.port, at: now } : auto.opened;
      patch({ ports: next, seenSites: seen.after, auto: { shown: true, seenAt: decision.seenAt, opened } });
      if (target) void open(asPort(target), merged);
    },
    PORTS_POLL,
  );

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (state.kind === 'opening') return;
    let target: TPreviewTarget | null;
    try {
      target = parsePreviewTarget(input);
    } catch (error) {
      setState({ kind: 'error', message: failureMessage(error) });
      return;
    }
    if (target === null) {
      setState({ kind: 'error', message: t('inspector.previewIsland.badTarget') });
      return;
    }
    openByHand(target);
  };

  const shownPort =
    (state.kind === 'open' || state.kind === 'opening') && state.target.kind === 'port'
      ? state.target.port
      : null;
  const menu = [
    {
      kind: TW_MENU_KIND.action,
      id: 'manual',
      label: manual ? t('inspector.previewIsland.hideManual') : t('inspector.previewIsland.showManual'),
      onSelect: () => setManual(!manual),
    },
    ...history.map((target) => ({
      kind: TW_MENU_KIND.action,
      id: `url:${targetAddress(target)}`,
      label: targetAddress(target),
      onSelect: () => openByHand(target),
    })),
  ];

  return (
    <div className={expanded ? 'tw-preview tw-preview-expanded' : 'tw-preview'}>
      {!bare && (
        <div className="tw-preview-bar">
          <span className="tw-preview-address">
            {state.kind === 'open' ? targetLabel(state.target) : t('inspector.previewIsland.devServers')}
          </span>
          {state.kind === 'open' && !floating && (
            <TwButton
              type="button"
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              aria-label={t('inspector.previewIsland.openInNewTab')}
              title={t('inspector.previewIsland.openInNewTab')}
              disabled={openingTab}
              onClick={() =>
                void openInTab(
                  reopenTarget(state),
                  state.target.kind === 'port' ? neighboursIn(rows, state.target.port) : [],
                )
              }
            >
              <ExternalLink />
            </TwButton>
          )}
          <TwMenuButton
            items={menu}
            menuLabel={t('inspector.previewIsland.more')}
            align={LAYER_ALIGN.end}
            className="tw-icon-button tw-preview-more"
            aria-label={t('inspector.previewIsland.more')}
            title={t('inspector.previewIsland.more')}
          >
            <MoreHorizontal />
          </TwMenuButton>
        </div>
      )}
      {manual && (
        <form className="tw-preview-bar" onSubmit={submit}>
          <input
            className="tw-preview-input"
            value={input}
            onChange={(event) => setInput(event.target.value)}
            placeholder={t('inspector.previewIsland.inputPlaceholder')}
            aria-label={t('inspector.previewIsland.inputLabel')}
          />
          <TwButton
            type="submit"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            disabled={state.kind === 'opening'}
          >
            {t('inspector.previewIsland.open')}
          </TwButton>
        </form>
      )}
      {manual && history.length > 0 && (
        <ul className="tw-preview-history" aria-label={t('inspector.previewIsland.recent')}>
          {history.map((target) => (
            <li key={targetAddress(target)} className="tw-preview-history-item">
              <TwButton
                type="button"
                size={BUTTON_SIZE.sm}
                variant={BUTTON_VARIANT.ghost}
                title={targetAddress(target)}
                onClick={() => openByHand(target)}
              >
                {targetAddress(target).replace(/^https:\/\//, '')}
              </TwButton>
            </li>
          ))}
        </ul>
      )}
      {gate}
      {tabError && (
        <p className="tw-preview-error" role="alert">
          {tabError}
        </p>
      )}
      {(state.kind === 'open' || state.kind === 'opening') && (
        <div className="tw-preview-detached" role="status">
          <span>
            {floating
              ? t('inspector.previewIsland.openFloating')
              : state.kind === 'opening'
                ? t('inspector.previewIsland.openingDots', { target: targetLabel(state.target) })
                : t('inspector.previewIsland.opened', { target: targetLabel(state.target) })}
          </span>
          <TwButton
            type="button"
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.ghost}
            onClick={() => {
              if (floating) patch({ detached: false });
              openPaneTab('browser');
            }}
          >
            {floating ? t('inspector.previewIsland.dock') : t('inspector.previewIsland.show')}
          </TwButton>
        </div>
      )}
      {state.kind === 'error' && (
        <p className="tw-preview-error" role="alert">
          {state.message}
        </p>
      )}
      {state.kind === 'taken' && !floating && (
        <PreviewBody
          previewKey={key}
          state={state}
          onReopenTaken={(taken) => openByHand(reopenTarget(taken))}
          onStale={() => {}}
          onEntered={() => {}}
          onReload={() => {}}
          onOpenUrl={(target) => openByHand(target)}
        />
      )}
      {portsError && (
        <p className="tw-preview-error" role="alert">
          {portsError}
        </p>
      )}
      {rows === null && !portsError && (
        <div
          className="tw-preview-ports-loading tw-skeleton"
          role="status"
          aria-label={t('inspector.previewIsland.searching')}
        />
      )}
      {rows !== null && rows.length === 0 && (
        <p className="tw-inspector-muted">{t('inspector.previewIsland.noServers')}</p>
      )}
      <MachineLoadNotice notice={notice} />
      {rows !== null && rows.length > 0 && (
        <PortList
          rows={rows}
          currentPort={shownPort}
          onOpen={(row) => openByHand(asPort({ port: row.port, path: row.path }))}
          top={load?.top}
          stopping={stopping}
          stopped={stopped}
          onStop={(proc) => void stop(proc)}
        />
      )}
    </div>
  );
};
