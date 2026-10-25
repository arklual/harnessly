// DevTools of an open design, under its page: the elements of the page's DOM (read through the board's own script), the
// console and the network of the page's preview slot, and the runtime's record of the board as the server's. The runtime
// keeps the record (the slot's proxy and the bridge in the page write it; the pane and the agents read it), the panel
// polls it while it is open, filters it by type and by words, and sends any line to the chat. The throttling and the cache
// are the slot's own settings, changed through the runtime.
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import {
  fetchDevtools,
  patchDevtoolsSettings,
  THROTTLE,
  type TThrottlePreset,
} from '@/next/core/net/preview-devtools';
import { RuntimeError } from '@/next/core/net/runtime-json';
import {
  applySnapshot,
  consoleRows,
  DEV_TAB,
  DEV_TABS,
  DEV_TYPES,
  emptyLog,
  errorCount,
  networkFoot,
  networkRows,
  type IDevRow,
  type IDevtoolsLog,
  type TDevTab,
} from '@/next/features/designly/devtools-data';
import { allServerRows, lastAt, serverErrors, serverRows } from '@/next/features/designly/server-data';
import SiteElements from '@/next/features/designly/site-elements';
import type { IDomBridge } from '@/next/features/designly/use-dom-bridge';
import { useServerSources } from '@/next/features/designly/use-server-sources';
import type { MachineId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import type { TPresenceState } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

/** An open panel reads the record about once a second, as a log should feel live. */
const POLL_MS = 1000;
/** The runtime has no record of a slot before its page's first request: that is an empty log, not a failure. */
const NOT_YET = new Set(['invalid slot', 'no preview slot yet']);

const PRESETS = [THROTTLE.none, THROTTLE.fast4g, THROTTLE.slow4g, THROTTLE.g3, THROTTLE.offline] as const;

/** The slot's record while the panel is open; settings are pessimistic: what the runtime answers is what is shown. */
const useDevtools = (machine: MachineId, slot: string | null, open: boolean) => {
  const [stored, setLog] = useState<IDevtoolsLog>(() => emptyLog(slot));
  const log = stored.slot === slot ? stored : emptyLog(slot);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const cursor = useRef({ slot, at: 0 });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!open || slot === null) return;
    const abort = new AbortController();
    let timer = 0;
    const poll = async () => {
      if (cursor.current.slot !== slot) cursor.current = { slot, at: 0 };
      const since = cursor.current.at;
      try {
        let snapshot = await fetchDevtools(machine, slot, since, abort.signal);
        let from = since;
        // The runtime started over: what is known belongs to a record that is gone, the new one is read from its start.
        if (snapshot.cursor < since) {
          from = 0;
          snapshot = await fetchDevtools(machine, slot, 0, abort.signal);
        }
        cursor.current = { slot, at: snapshot.cursor };
        setLog(
          (current) => applySnapshot(current.slot === slot ? current : emptyLog(slot), snapshot, from).log,
        );
        setError(null);
      } catch (failure) {
        if (abort.signal.aborted) return;
        const said = failure instanceof RuntimeError ? failure.message : '';
        setError(NOT_YET.has(said) ? null : t('next.site.dev.unavailable'));
      }
      if (!abort.signal.aborted) timer = window.setTimeout(poll, POLL_MS);
    };
    void poll();
    return () => {
      abort.abort();
      window.clearTimeout(timer);
    };
  }, [machine, slot, open, attempt]);

  const change = async (patch: { throttle?: { preset: TThrottlePreset }; disableCache?: boolean }) => {
    if (slot === null || saving) return;
    setSaving(true);
    try {
      const settings = await patchDevtoolsSettings(machine, slot, patch);
      setLog((current) => ({ ...(current.slot === slot ? current : emptyLog(slot)), settings }));
    } catch (failure) {
      toast(t('next.site.dev.settingsFailed'), {
        tone: 'error',
        description: failure instanceof Error ? failure.message : undefined,
      });
    } finally {
      setSaving(false);
    }
  };

  return { log, error, saving, change, retry: () => setAttempt((n) => n + 1) };
};

export interface IThrottleMenu {
  mounted: boolean;
  state: TPresenceState;
  onToggle: () => void;
  onClose: () => void;
}

export interface IInspectSwitch {
  on: boolean;
  /** There is no page to inspect (it has not loaded, or it is not a board). */
  disabled: boolean;
  onToggle: () => void;
}

export interface ISiteDevtoolsProps {
  machine: MachineId;
  /** The page's preview slot (its origin); null while the slot opens. */
  slot: string | null;
  /** The design, and the board on the page: the runtime's record of the server is theirs. */
  artifact: string;
  board: string;
  open: boolean;
  tab: TDevTab;
  onTab: (tab: TDevTab) => void;
  /** The page's tree for the elements tab. */
  dom: IDomBridge;
  inspect: IInspectSwitch;
  throttle: IThrottleMenu;
  /** A line goes to the chat; without a chat there is nowhere to send it. */
  onSend?: (words: string) => void;
  onClose: () => void;
  /** The top edge takes the pointer: the panel's height follows it. */
  onGrab: (event: ReactPointerEvent<HTMLElement>) => void;
}

/** The rows of a tab that lists rows, the number of entries on it that were not cleared, and what the person has cleared. */
const listOf = (
  tab: TDevTab,
  log: IDevtoolsLog,
  server: ReturnType<typeof allServerRows>,
  filter: { clearedAt: number; type: string; query: string },
): { rows: IDevRow[]; fresh: number } => {
  if (tab === DEV_TAB.console) {
    return {
      rows: consoleRows(log.console, filter),
      fresh: log.console.filter((e) => e.id > filter.clearedAt).length,
    };
  }
  if (tab === DEV_TAB.network) {
    return {
      rows: networkRows(log.network, filter),
      fresh: log.network.filter((e) => e.id > filter.clearedAt).length,
    };
  }
  if (tab === DEV_TAB.server) {
    return { rows: serverRows(server, filter), fresh: server.filter((r) => r.at > filter.clearedAt).length };
  }
  return { rows: [], fresh: 0 };
};

const SiteDevtools = ({
  machine,
  slot,
  artifact,
  board,
  open,
  tab,
  onTab,
  dom,
  inspect,
  throttle,
  onSend,
  onClose,
  onGrab,
}: ISiteDevtoolsProps) => {
  const { log, error, saving, change, retry } = useDevtools(machine, slot, open);
  const record = useServerSources(machine, artifact, open);
  const server = allServerRows(
    { requests: log.network, pageLog: record.pageLog, check: record.check },
    board,
  );
  const [type, setType] = useState('all');
  const [query, setQuery] = useState('');
  /** Entries up to this id (a time, for the server) were cleared on a tab: hidden here, left in the runtime's record. */
  const [cleared, setCleared] = useState<Record<TDevTab, number>>({
    elements: 0,
    console: 0,
    network: 0,
    server: 0,
  });

  const pickTab = (next: TDevTab) => {
    onTab(next);
    setType('all');
    setQuery('');
  };

  const filter = { clearedAt: cleared[tab], type, query: query.trim().toLowerCase() };
  const { rows, fresh } = listOf(tab, log, server, filter);
  let emptyText = t('next.site.dev.nothing');
  if (fresh === 0 && cleared[tab] > 0) emptyText = t('next.site.dev.cleared');
  else if (fresh === 0) emptyText = t(`next.site.dev.empty.${tab}`);

  const clear = () => {
    const at = tab === DEV_TAB.server ? lastAt(server) : log.cursor;
    setCleared((c) => ({ ...c, [tab]: at }));
  };
  const errorsOf = (id: TDevTab) =>
    id === DEV_TAB.server ? serverErrors(server, cleared.server) : errorCount(log, id, cleared[id]);

  const preset = log.settings.throttle.preset;
  const slowed = preset !== THROTTLE.none;
  let foot = '';
  if (tab === DEV_TAB.network) foot = networkFoot(log.network, cleared[tab]);
  if (tab === DEV_TAB.server)
    foot = t(record.logFailed ? 'next.site.dev.server.footNoLog' : 'next.site.dev.server.foot');
  const headLabel = t(`next.site.dev.tab.${tab}`);

  return (
    <div className="hx-devtools" data-open={open} inert={open ? undefined : true}>
      <div className="hx-devtools__grab" aria-hidden="true" onPointerDown={onGrab} />
      <div className="hx-devtools__head">
        <button
          type="button"
          className="hx-head-btn hx-tipped"
          aria-pressed={inspect.on}
          aria-disabled={inspect.disabled}
          aria-label={t('next.site.inspect')}
          onClick={() => !inspect.disabled && inspect.onToggle()}
        >
          <Icon name="square-mouse-pointer" size="sm" />
          <span className="hx-tip" data-pos="below" data-align="start">
            {t('next.site.dev.inspectTip')}
            <kbd>⇧⌘C</kbd>
          </span>
        </button>
        <span className="hx-devtools__sep" />
        {DEV_TABS.map((id) => {
          const errors = errorsOf(id);
          return (
            <button
              key={id}
              type="button"
              className="hx-devtools__tab"
              aria-selected={id === tab}
              onClick={() => pickTab(id)}
            >
              {t(`next.site.dev.tab.${id}`)}
              {errors > 0 && (
                <span className="hx-devtools__badge" data-lv="error">
                  {errors}
                </span>
              )}
            </button>
          );
        })}
        <span className="hx-spacer" />
        {tab !== DEV_TAB.elements && (
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.site.dev.clear')}
            onClick={clear}
          >
            <Icon name="ban" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.site.dev.clear')}
            </span>
          </button>
        )}
        <button type="button" className="hx-head-btn" aria-label={t('next.site.dev.close')} onClick={onClose}>
          <Icon name="x" size="sm" />
        </button>
      </div>
      {tab !== DEV_TAB.elements && (
        <div className="hx-devtools__tools">
          {tab === DEV_TAB.network && (
            <>
              <div className="hx-browser__anchor">
                <button
                  type="button"
                  className="hx-devchip"
                  aria-expanded={throttle.mounted && throttle.state === 'open'}
                  data-on={slowed}
                  onClick={throttle.onToggle}
                >
                  <Icon name="gauge" size="sm" />
                  {t(`next.site.dev.throttle.${slowed ? preset : 'free'}`)}
                </button>
                {throttle.mounted && (
                  <div className="hx-site-menu hx-site-menu--up" data-state={throttle.state} role="menu">
                    {PRESETS.map((id) => (
                      <button
                        key={id}
                        type="button"
                        role="menuitemradio"
                        className="hx-option hx-devopt"
                        aria-selected={id === preset}
                        aria-disabled={saving}
                        onClick={() => {
                          throttle.onClose();
                          if (id !== preset) void change({ throttle: { preset: id } });
                        }}
                      >
                        <Icon name="check" size="sm" className="hx-devopt__check" />
                        <span className="hx-devopt__name">{t(`next.site.dev.throttle.${id}`)}</span>
                        <span className="hx-devopt__meta">{t(`next.site.dev.throttleMeta.${id}`)}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
              <button
                type="button"
                className="hx-devchip"
                data-on={log.settings.disableCache}
                aria-pressed={log.settings.disableCache}
                aria-disabled={saving}
                onClick={() => void change({ disableCache: !log.settings.disableCache })}
              >
                <Icon name="database" size="sm" />
                {t('next.site.dev.noCache')}
              </button>
            </>
          )}
          <label className="hx-devfilter">
            <Icon name="search" size="sm" />
            <input
              type="text"
              autoComplete="off"
              placeholder={t('next.site.dev.filter')}
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
            />
          </label>
          <div className="hx-devtypes">
            {DEV_TYPES[tab].map((id) => (
              <button
                key={id}
                type="button"
                className="hx-devtype"
                aria-pressed={id === type}
                onClick={() => setType(id)}
              >
                {t(`next.site.dev.type.${id}`)}
              </button>
            ))}
          </div>
        </div>
      )}
      <div className="hx-devtools__body" data-tab={tab}>
        {tab === DEV_TAB.elements && <SiteElements dom={dom} file={board} onSend={onSend} />}
        {tab === DEV_TAB.network && rows.length > 0 && (
          <div className="hx-devrow hx-devrow--head">
            <span>{t('next.site.dev.col.name')}</span>
            <span>{t('next.site.dev.col.status')}</span>
            <span>{t('next.site.dev.col.type')}</span>
            <span>{t('next.site.dev.col.size')}</span>
            <span>{t('next.site.dev.col.time')}</span>
            <span />
          </div>
        )}
        {rows.map((row) => (
          <div key={row.key} className="hx-devrow" data-tab={tab} data-lv={row.lv} title={row.title}>
            {row.cells.map((cell, i) => (
              <span key={i}>{cell}</span>
            ))}
            {onSend ? (
              <button
                type="button"
                className="hx-devrow__send hx-tipped"
                aria-label={t('next.site.dev.toChat')}
                onClick={() => onSend(`${headLabel}: ${row.detail}`)}
              >
                <Icon name="message-square-plus" size="sm" />
                <span className="hx-tip" data-align="end">
                  {t('next.site.dev.sendToChat')}
                </span>
              </button>
            ) : (
              <span />
            )}
          </div>
        ))}
        {tab !== DEV_TAB.elements && rows.length === 0 && error !== null && tab !== DEV_TAB.server && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.pane}
            title={t('next.load.devtools')}
            reason={error}
            onRetry={retry}
          />
        )}
        {tab !== DEV_TAB.elements && rows.length === 0 && (error === null || tab === DEV_TAB.server) && (
          <div className="hx-devtools__empty">{emptyText}</div>
        )}
      </div>
      <div className="hx-devtools__foot" title={foot}>
        {foot}
      </div>
    </div>
  );
};

export default SiteDevtools;
