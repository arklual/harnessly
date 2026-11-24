// «Регулярные задачи» (/routines): the person's routines — what the hub runs again and again for them —
// with the history of every run (what it came to, what it made, how it went), the settings and the memory. A routine
// is made and changed in words: the field at the bottom starts a chat with the agent, which uses `harnessly routine`.
import { useMemo, useState, type ReactNode } from 'react';
import { CardReply } from '@/next/bindings/reply-field';
import { ReplyMachine } from '@/next/features/composer/reply-field';
import {
  AUTONOMIES,
  CHANNELS,
  MODELS,
  WHENS,
  deleteRoutine,
  patchRoutine,
  runRoutine,
  useRoutines,
  useRuns,
  type IRoutine,
  type IRun,
  type TChannel,
  type TOutcome,
} from '@/next/features/routines/api';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { NEW_CHAT, setDraftText } from '@/next/stores/drafts';
import { Button } from '@/next/ui/button';
import Icon, { type TIconName } from '@/next/ui/icon';

type TTab = 'history' | 'settings' | 'memory';

const dateFmt = (iso: string, o: Intl.DateTimeFormatOptions) => {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString(undefined, o);
};
const when = (iso: string) =>
  dateFmt(iso, { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const hm = (iso: string) => dateFmt(iso, { hour: '2-digit', minute: '2-digit' });
const day = (iso: string) => dateFmt(iso, { weekday: 'short', day: 'numeric', month: 'short' });

/** What a run came to, for its dot and its line: the outcome, or the state of a run that has none yet. */
const runState = (status: string, outcome?: TOutcome): 'ok' | 'quiet' | 'failed' | 'running' | 'queued' => {
  if (outcome) return outcome;
  if (status === 'failed') return 'failed';
  if (status === 'running') return 'running';
  return 'queued';
};

const RUN_ICON: Record<ReturnType<typeof runState>, TIconName> = {
  ok: 'circle-check',
  quiet: 'circle-dashed',
  failed: 'circle-x',
  running: 'loader-circle',
  queued: 'clock',
};

const ART_ICON: Record<string, TIconName> = {
  doc: 'file-text',
  file: 'file-text',
  chart: 'chart-column',
  mr: 'git-pull-request',
  board: 'layout-dashboard',
  link: 'link',
};

/** The routine's status for its dot and pill. */
const statusOf = (r: IRoutine): 'on' | 'paused' | 'error' | 'once' => {
  if (!r.enabled) return r.pausedReason ? 'error' : 'paused';
  if (r.routine.once) return 'once';
  return 'on';
};

const artLabel = (kind: string) => {
  const key = `next.routines.art.${kind}`;
  const said = t(key);
  return said === key ? kind : said;
};

const startChat = (text: string) => {
  setDraftText(NEW_CHAT, text);
  goTo({ kind: ROUTE.home });
};

interface IPageProps {
  machine: MachineId;
  id?: string;
  run?: string;
  onOpenDrawer: () => void;
}

const RoutinesPage = ({ machine, id, run, onOpenDrawer }: IPageProps) => {
  const { state, reload } = useRoutines(machine);
  const routines = state.k === 'ready' ? state.v.routines : [];
  const [picked, setPicked] = useState<string | null>(id ?? null);
  const [detail, setDetail] = useState(Boolean(id));
  // An address with another routine (a notice's link) opens it: the change of the prop is taken while rendering.
  const [seenId, setSeenId] = useState(id);
  if (id !== seenId) {
    setSeenId(id);
    if (id) {
      setPicked(id);
      setDetail(true);
    }
  }
  const current = routines.find((r) => r.id === picked) ?? routines[0] ?? null;
  const [ask, setAsk] = useState('');

  const send = () => {
    const text = ask.trim();
    if (!text) return;
    startChat(
      current
        ? t('next.routines.askAbout', { name: current.name, id: current.id, text })
        : t('next.routines.askNew', { text }),
    );
  };

  return (
    <div className="hx-rt">
      <div className="hx-rt__bar">
        <button
          type="button"
          className="hx-head-btn hx-rt__menu"
          aria-label={t('next.header.openMenu')}
          onClick={onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
        <span className="hx-rt__bar-title">{t('next.routines.title')}</span>
      </div>
      <div className="hx-rt__in">
        {state.k === 'loading' && <div className="hx-rt__note">{t('next.routines.loading')}</div>}
        {state.k === 'failed' && (
          <div className="hx-rt__empty">
            <Icon name="triangle-alert" />
            <h2>{t('next.routines.offTitle')}</h2>
            <p>{t('next.routines.offText')}</p>
            <p className="hx-rt__err">{state.error}</p>
            <Button icon="refresh-cw" onClick={reload}>
              {t('next.routines.retry')}
            </Button>
          </div>
        )}
        {state.k === 'ready' && routines.length === 0 && <Empty />}
        {state.k === 'ready' && current && (
          <div className="hx-rt__lay" data-detail={detail}>
            <aside className="hx-rt__list">
              {routines.map((r) => (
                <RoutineRow
                  key={r.id}
                  r={r}
                  on={r.id === current.id}
                  onPick={() => {
                    setPicked(r.id);
                    setDetail(true);
                  }}
                />
              ))}
              <button
                type="button"
                className="hx-rt__new"
                onClick={() => startChat(t('next.routines.newDraft'))}
              >
                <Icon name="sparkles" size="sm" />
                <span>{t('next.routines.newCard')}</span>
              </button>
            </aside>
            <Detail
              key={current.id}
              machine={machine}
              r={current}
              openRun={run}
              tick={state}
              reload={reload}
              onBack={() => setDetail(false)}
            />
          </div>
        )}
      </div>
      {state.k === 'ready' && (
        <div className="hx-rt__dock">
          {/* The product's own field (the chat's composer, with the machine's voice), not a look-alike of it. */}
          <ReplyMachine.Provider value={machine}>
            <CardReply
              value={ask}
              onChange={setAsk}
              busy={false}
              placeholder={current ? t('next.routines.askPlaceholder') : t('next.routines.newPlaceholder')}
              onSend={send}
              sendLabel={t('next.routines.send')}
            />
          </ReplyMachine.Provider>
        </div>
      )}
    </div>
  );
};

const Empty = () => (
  <div className="hx-rt__empty">
    <Icon name="clock" />
    <h2>{t('next.routines.emptyTitle')}</h2>
    <p>{t('next.routines.emptyText')}</p>
    <div className="hx-rt__chips">
      {(['idea1', 'idea2', 'idea3'] as const).map((k) => (
        <button
          key={k}
          type="button"
          className="hx-rt__chip"
          onClick={() => startChat(t(`next.routines.${k}`))}
        >
          {t(`next.routines.${k}`)}
        </button>
      ))}
    </div>
  </div>
);

const outcomeWords = (s: ReturnType<typeof runState>) => t(`next.routines.run.${s}`);

const RoutineRow = ({ r, on, onPick }: { r: IRoutine; on: boolean; onPick: () => void }) => {
  const dots = [...r.last].reverse();
  const next = r.next[0];
  const line = [
    r.routine.words || r.trigger.schedule?.cron || '',
    r.enabled ? (next ? when(next) : '') : t('next.routines.paused'),
  ]
    .filter(Boolean)
    .join(' · ');
  return (
    <button type="button" className="hx-rt-row" aria-current={on} onClick={onPick}>
      <span className="hx-rt-dot" data-s={statusOf(r)} />
      <b>{r.name}</b>
      <span className="hx-rt-runs" aria-hidden={dots.length === 0}>
        {dots.map((d) => {
          const s = runState(d.status, d.outcome);
          return <i key={d.id} data-s={s} title={`${when(d.at)} · ${outcomeWords(s)}`} />;
        })}
      </span>
      <small>{line}</small>
    </button>
  );
};

interface IDetailProps {
  machine: MachineId;
  r: IRoutine;
  openRun?: string;
  tick: unknown;
  reload: () => void;
  onBack: () => void;
}

const Detail = ({ machine, r, openRun, tick, reload, onBack }: IDetailProps) => {
  const [tab, setTab] = useState<TTab>('history');
  const [busy, setBusy] = useState<string | null>(null);
  const [said, setSaid] = useState<string | null>(null);
  const [menu, setMenu] = useState(false);
  const runs = useRuns(machine, r.id, tick);
  const st = statusOf(r);
  const act = async (what: string, fn: () => Promise<unknown>, done: string) => {
    setBusy(what);
    setSaid(null);
    try {
      await fn();
      setSaid(done);
      reload();
    } catch (e) {
      setSaid(String((e as Error)?.message ?? e));
    } finally {
      setBusy(null);
    }
  };
  const save = (patch: Parameters<typeof patchRoutine>[2]) =>
    act('save', () => patchRoutine(machine, r.id, patch), t('next.routines.saved'));
  const running = runs?.some((x) => x.status === 'running' || x.status === 'queued') ?? false;

  return (
    <section className="hx-rt-det">
      <button
        type="button"
        className="hx-btn hx-rt-det__back"
        data-variant="ghost"
        data-size="sm"
        onClick={onBack}
      >
        <Icon name="arrow-left" size="sm" />
        {t('next.routines.all')}
      </button>
      <div className="hx-rt-det__title">
        <h1>{r.name}</h1>
        <span className="hx-rt-pill" data-s={running ? 'running' : st}>
          {running ? t('next.routines.pill.running') : t(`next.routines.pill.${st}`)}
        </span>
      </div>
      <p className="hx-rt-det__when">
        <b>{r.routine.words || r.trigger.schedule?.cron}</b>
        {r.enabled && r.next[0] && <> · {t('next.routines.nextAt', { at: when(r.next[0]) })}</>}
      </p>
      {r.pausedReason && (
        <div className="hx-rt-alert">
          <Icon name="triangle-alert" size="sm" />
          <span>{r.pausedReason}</span>
        </div>
      )}
      <div className="hx-rt-det__acts">
        <Button
          variant="primary"
          size="sm"
          icon="play"
          busy={busy === 'run'}
          disabled={!r.enabled}
          onClick={() => void act('run', () => runRoutine(machine, r.id), t('next.routines.queued'))}
        >
          {t('next.routines.runNow')}
        </Button>
        <Button
          size="sm"
          icon={r.enabled ? 'pause' : 'play'}
          busy={busy === 'save'}
          onClick={() => void save({ enabled: !r.enabled })}
        >
          {r.enabled ? t('next.routines.pause') : t('next.routines.resume')}
        </Button>
        <span className="hx-rt-more">
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            aria-label={t('next.routines.more')}
            aria-expanded={menu}
            onClick={() => setMenu(!menu)}
          >
            <Icon name="ellipsis" size="sm" />
          </button>
          {menu && (
            <>
              <div className="hx-rt-menu-veil" onClick={() => setMenu(false)} />
              <div className="hx-rt-menu" role="menu">
                {r.routine.chatId && (
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => goTo({ kind: ROUTE.chat, id: r.routine.chatId as string })}
                  >
                    <Icon name="message-square" size="sm" />
                    <span>{t('next.routines.openChat')}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => {
                    void navigator.clipboard?.writeText(`${window.location.origin}/routines/${r.id}`);
                    setMenu(false);
                    setSaid(t('next.routines.linkCopied'));
                  }}
                >
                  <Icon name="link" size="sm" />
                  <span>{t('next.routines.copyLink')}</span>
                </button>
                <hr />
                <button
                  type="button"
                  role="menuitem"
                  data-danger="true"
                  onClick={() => {
                    setMenu(false);
                    if (window.confirm(t('next.routines.deleteAsk', { name: r.name }))) {
                      void act('delete', () => deleteRoutine(machine, r.id), t('next.routines.deleted'));
                    }
                  }}
                >
                  <Icon name="trash-2" size="sm" />
                  <span>{t('next.routines.delete')}</span>
                </button>
              </div>
            </>
          )}
        </span>
        {said && <span className="hx-rt-said">{said}</span>}
      </div>

      <div className="hx-rt-stats">
        <div>
          <small>{t('next.routines.stat.runs')}</small>
          <b>{r.stats.runs}</b>
        </div>
        <div>
          <small>{t('next.routines.stat.ok')}</small>
          <b>{r.stats.ok}</b>
        </div>
        <div>
          <small>{t('next.routines.stat.quietFailed')}</small>
          <b>
            {r.stats.quiet} / {r.stats.failed}
          </b>
        </div>
        <div>
          <small>{t('next.routines.stat.model')}</small>
          <b>{r.model || t('next.routines.modelDefault')}</b>
        </div>
      </div>

      <div className="hx-rt-tabs" role="tablist">
        {(['history', 'settings', 'memory'] as const).map((k) => (
          <button key={k} type="button" role="tab" aria-selected={tab === k} onClick={() => setTab(k)}>
            {t(`next.routines.tab.${k}`)}
            {k === 'history' && runs && runs.length > 0 && <small>{runs.length}</small>}
          </button>
        ))}
      </div>

      {tab === 'history' && <History runs={runs} openRun={openRun} />}
      {tab === 'settings' && <Settings r={r} busy={busy === 'save'} save={save} />}
      {tab === 'memory' && (
        <div className="hx-rt-mem">
          {r.state?.trim() ? (
            <pre>{r.state}</pre>
          ) : (
            <p className="hx-rt__note">{t('next.routines.memEmpty')}</p>
          )}
          <p className="hx-rt__note">{t('next.routines.memHint')}</p>
          {r.state?.trim() && (
            <Button
              size="sm"
              variant="ghost"
              icon="trash-2"
              busy={busy === 'save'}
              onClick={() => void save({ state: '' })}
            >
              {t('next.routines.memClear')}
            </Button>
          )}
        </div>
      )}
    </section>
  );
};

const History = ({ runs, openRun }: { runs: IRun[] | null; openRun?: string }) => {
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [deep, setDeep] = useState<Record<string, boolean>>({});
  const days = useMemo(() => {
    const out: { day: string; runs: IRun[] }[] = [];
    for (const r of runs ?? []) {
      const d = day(r.createdAt);
      const last = out[out.length - 1];
      if (last && last.day === d) last.runs.push(r);
      else out.push({ day: d, runs: [r] });
    }
    return out;
  }, [runs]);
  if (runs === null) return <p className="hx-rt__note">{t('next.routines.loading')}</p>;
  if (runs.length === 0) return <p className="hx-rt__note">{t('next.routines.noRuns')}</p>;
  const first = runs[0]?.id;
  return (
    <div className="hx-rt-hist">
      {days.map((d) => (
        <div key={d.day}>
          <div className="hx-rt-day">{d.day}</div>
          {d.runs.map((run) => {
            const s = runState(run.status, run.outcome);
            const res = run.result ?? {};
            const isOpen =
              open[run.id] ?? (run.id === openRun || (run.id === first && !openRun && s !== 'queued'));
            const summary =
              res.summary || run.summary || (s === 'failed' ? res.error || run.error : '') || outcomeWords(s);
            const fails = (res.checks ?? []).filter((c) => !c.ok);
            const passed = (res.checks ?? []).filter((c) => c.ok).length;
            const sum = [
              res.checks && res.checks.length > 0
                ? t('next.routines.checksOf', { n: String(passed), of: String(res.checks.length) })
                : '',
              res.delivered && res.delivered.length > 0
                ? t('next.routines.sentTo', { to: res.delivered.join(', ') })
                : '',
            ]
              .filter(Boolean)
              .join(' · ');
            const isDeep = deep[run.id] ?? s === 'running';
            return (
              <div key={run.id}>
                <button
                  type="button"
                  className="hx-rt-ev"
                  data-open={isOpen}
                  onClick={() => setOpen({ ...open, [run.id]: !isOpen })}
                >
                  <time>{hm(run.createdAt)}</time>
                  <span className="hx-rt-ev__ic" data-s={s}>
                    <Icon name={RUN_ICON[s]} size="sm" />
                  </span>
                  <span className="hx-rt-ev__t">
                    <b>{outcomeWords(s)}.</b> <span>{summary}</span>
                  </span>
                  <span className="hx-rt-ev__m">
                    {[res.manual ? t('next.routines.manual') : '', res.model].filter(Boolean).join(' · ')}
                    <Icon name="chevron-right" size="sm" />
                  </span>
                </button>
                {isOpen && (
                  <div className="hx-rt-x">
                    {(res.summary || (res.artifacts?.length ?? 0) > 0) && (
                      <div className="hx-rt-result">
                        {res.summary && <p>{res.summary}</p>}
                        {(res.artifacts?.length ?? 0) > 0 && (
                          <div className="hx-rt-arts">
                            {res.artifacts?.map((a, i) => (
                              <a
                                key={i}
                                className="hx-file"
                                href={a.url || undefined}
                                target="_blank"
                                rel="noreferrer"
                              >
                                <span className="hx-file__ico" data-tone="accent">
                                  <Icon name={ART_ICON[a.kind] ?? 'file-text'} />
                                </span>
                                <span className="hx-file__text">
                                  <span className="hx-file__name">{a.title}</span>
                                  <span className="hx-file__meta">
                                    {[artLabel(a.kind), a.meta].filter(Boolean).join(' · ')}
                                  </span>
                                </span>
                              </a>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                    {(fails.length > 0 || (s === 'failed' && (res.error || run.error))) && (
                      <div className="hx-rt-fails">
                        {fails.map((c, i) => (
                          <div key={i} className="hx-rt-check" data-ok="false">
                            <Icon name="circle-x" size="sm" />
                            <div>
                              {c.label}
                              {c.out && <div className="hx-rt-out">{c.out}</div>}
                            </div>
                          </div>
                        ))}
                        {fails.length === 0 && (
                          <div className="hx-rt-check" data-ok="false">
                            <Icon name="circle-x" size="sm" />
                            <div>{res.error || run.error}</div>
                          </div>
                        )}
                      </div>
                    )}
                    <div className="hx-rt-sumline">
                      <span>{sum}</span>
                      <span className="hx-spacer" />
                      <button
                        type="button"
                        className="hx-rt-link"
                        onClick={() => setDeep({ ...deep, [run.id]: !isDeep })}
                      >
                        {isDeep ? t('next.routines.hideSteps') : t('next.routines.showSteps')}
                      </button>
                      {run.conversationId && (
                        <button
                          type="button"
                          className="hx-rt-link"
                          data-strong="true"
                          onClick={() => goTo({ kind: ROUTE.chat, id: run.conversationId as string })}
                        >
                          <Icon name="message-square" size="sm" />
                          {t('next.routines.inChat')}
                        </button>
                      )}
                    </div>
                    {isDeep && <Steps run={run} />}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
};

const Steps = ({ run }: { run: IRun }) => {
  const res = run.result ?? {};
  const s = runState(run.status, run.outcome);
  const manual = run.fingerprint.startsWith('manual:');
  return (
    <div className="hx-rt-steps">
      <Step
        icon="clock"
        ok
        t={manual ? t('next.routines.step.manual') : t('next.routines.step.slot')}
        m={when(run.createdAt)}
      />
      <Step
        icon="server"
        ok={Boolean(run.dispatchedAt)}
        t={
          run.dispatchedAt
            ? t('next.routines.step.machine', { m: run.machineId ?? '' })
            : t('next.routines.step.waiting')
        }
        m={run.dispatchedAt ? when(run.dispatchedAt) : ''}
      />
      <Step
        icon={s === 'running' ? 'loader-circle' : 'sparkles'}
        ok={s === 'running' ? null : Boolean(run.finishedAt)}
        t={t('next.routines.step.agent', { model: res.model ?? '' })}
        m={run.finishedAt ? when(run.finishedAt) : ''}
      />
      {(res.checks?.length ?? 0) > 0 && (
        <div className="hx-rt-step">
          <Icon name="list-checks" size="sm" />
          <div>
            <span>{t('next.routines.step.checks')}</span>
            {res.checks?.map((c, i) => (
              <div key={i} className="hx-rt-check" data-ok={c.ok}>
                <Icon name={c.ok ? 'circle-check' : 'circle-x'} size="sm" />
                <div>
                  {c.label}
                  {c.out && <div className="hx-rt-out">{c.out}</div>}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
      <Step
        icon="send"
        ok={res.delivered ? res.delivered.length > 0 : null}
        t={t('next.routines.step.delivery')}
        m={
          res.delivered && res.delivered.length > 0
            ? res.delivered.join(', ')
            : t('next.routines.step.onlyChat')
        }
      />
    </div>
  );
};

const Step = ({ icon, ok, t: title, m }: { icon: TIconName; ok: boolean | null; t: string; m: string }) => (
  <div className="hx-rt-step" data-ok={ok === null ? 'none' : ok}>
    <Icon name={icon} size="sm" />
    <div>
      <span>{title}</span>
      {m && <small>{m}</small>}
    </div>
  </div>
);

interface ISettingsProps {
  r: IRoutine;
  busy: boolean;
  save: (patch: Parameters<typeof patchRoutine>[2]) => Promise<void>;
}

const Settings = ({ r, busy, save }: ISettingsProps) => {
  const spec = r.routine;
  const chans = new Set(spec.delivery.channels ?? []);
  const setSpec = (next: Partial<typeof spec>) => void save({ routine: { ...spec, ...next } });
  const toggle = (c: TChannel) => {
    const n = new Set(chans);
    if (n.has(c)) n.delete(c);
    else n.add(c);
    setSpec({ delivery: { ...spec.delivery, channels: CHANNELS.filter((x) => n.has(x)) } });
  };
  return (
    <div className="hx-rt-set" aria-busy={busy}>
      <div className="hx-rt-words">
        <Icon name="sparkles" size="sm" />
        <span>{t('next.routines.wordsHint')}</span>
      </div>
      <Sec title={t('next.routines.set.what')} hint={t('next.routines.set.whatHint')}>
        <div className="hx-rt-prompt">{r.promptTemplate}</div>
      </Sec>
      <Sec title={t('next.routines.set.when')} hint={r.trigger.schedule?.tz || 'Europe/Moscow'}>
        <div className="hx-rt-kv">
          <span>{t('next.routines.set.schedule')}</span>
          <b>{spec.words || r.trigger.schedule?.cron}</b>
          <code>{r.trigger.schedule?.cron}</code>
        </div>
        <div className="hx-rt-kv">
          <span>{t('next.routines.set.next')}</span>
          <span>{r.next.length > 0 ? r.next.map(when).join(', ') : '—'}</span>
        </div>
        <div className="hx-rt-kv">
          <span>{t('next.routines.set.ends')}</span>
          <span>
            {spec.once ? t('next.routines.set.once') : spec.until ? spec.until : t('next.routines.set.never')}
          </span>
        </div>
      </Sec>
      <Sec title={t('next.routines.set.where')} hint={t('next.routines.set.whereHint')}>
        <span>{r.machineId || t('next.routines.set.auto')}</span>
      </Sec>
      <Sec title={t('next.routines.set.model')} hint={t('next.routines.set.modelHint')}>
        <div className="hx-rt-seg" role="radiogroup">
          {MODELS.map((m) => (
            <button
              key={m || 'default'}
              type="button"
              role="radio"
              aria-checked={(r.model || '') === m}
              onClick={() => void save({ model: m || 'default' })}
            >
              {m || t('next.routines.modelDefault')}
            </button>
          ))}
        </div>
      </Sec>
      <Sec title={t('next.routines.set.checks')} hint={t('next.routines.set.checksHint')}>
        {(spec.checks?.length ?? 0) === 0 ? (
          <span className="hx-rt__note">{t('next.routines.set.noChecks')}</span>
        ) : (
          spec.checks?.map((c, i) => (
            <div key={i} className="hx-rt-check" data-ok="none">
              <Icon name="list-checks" size="sm" />
              <div>{c.label || `${c.kind}${c.value ? `: ${c.value}` : ''}`}</div>
            </div>
          ))
        )}
      </Sec>
      <Sec title={t('next.routines.set.result')} hint={t('next.routines.set.resultHint')}>
        <div className="hx-rt-seg" role="radiogroup">
          {WHENS.map((w) => (
            <button
              key={w}
              type="button"
              role="radio"
              aria-checked={(spec.delivery.when ?? 'changed') === w}
              onClick={() => setSpec({ delivery: { ...spec.delivery, when: w } })}
            >
              {t(`next.routines.when.${w}`)}
            </button>
          ))}
        </div>
        <div className="hx-rt-chans">
          {CHANNELS.map((c) => (
            <label key={c} className="hx-rt-chan">
              <input
                type="checkbox"
                checked={chans.has(c)}
                disabled={c === 'file' && !spec.delivery.file}
                onChange={() => toggle(c)}
              />
              <span>{t(`next.routines.chan.${c}`)}</span>
              {c === 'file' && spec.delivery.file && <code>{spec.delivery.file}</code>}
            </label>
          ))}
        </div>
      </Sec>
      <Sec
        title={t('next.routines.set.autonomy')}
        hint={t(`next.routines.autonomy.${spec.autonomy ?? 'read'}.hint`)}
      >
        <div className="hx-rt-seg" role="radiogroup">
          {AUTONOMIES.map((a) => (
            <button
              key={a}
              type="button"
              role="radio"
              aria-checked={(spec.autonomy ?? 'read') === a}
              onClick={() => setSpec({ autonomy: a })}
            >
              {t(`next.routines.autonomy.${a}`)}
            </button>
          ))}
        </div>
      </Sec>
    </div>
  );
};

const Sec = ({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) => (
  <div className="hx-rt-sec">
    <div className="hx-rt-sec__h">
      <b>{title}</b>
      {hint && <small>{hint}</small>}
    </div>
    <div className="hx-rt-sec__b">{children}</div>
  </div>
);

export default RoutinesPage;
