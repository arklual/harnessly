// App Inspector: what the person (or the author, for a dev install) sees about a running app — its manifest's rights,
// and for a dev install also the view's bridge calls (method, right, result or error code), the keys it keeps in
// storage and a reload. A section of the app's tab and of its page, not a tab of its own.
import { useCallback, useEffect, useState } from 'react';
import {
  callStorage,
  createViewSession,
  loadAppDecisions,
  loadAppJournal,
  type IAppDecision,
  type IJournalEntry,
  type IAppView,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import AppSetup from '@/next/features/app-views/app-setup';
import { clearLog, logKey, useBridgeLog } from '@/next/features/app-views/bridge-log';
import type { IBridgeLogEntry, TStorageScope, TTarget } from '@/next/features/app-views/bridge';
import { clock } from '@/next/i18n/format';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import { t } from '@/next/i18n';
import AccessList from '@/next/features/app-consent/access-list';
import '@/next/styles/app-main.css';

/** The scopes the inspector lists keys of: `doc` belongs to a Sinopia document and is refused until a view is bound to one. */
export const INSPECT_SCOPES = [
  'install',
  'user',
  'chat',
  'space',
] as const satisfies readonly TStorageScope[];

/** How a call ended, in a word the log shows: `ok` or the error code the view got. */
export const resultOf = (e: IBridgeLogEntry): string => (e.ok ? 'ok' : (e.code ?? 'error'));

/** The scopes worth asking for an app: it needs the right to storage, and `chat` a chat. */
export const scopesFor = (app: Pick<IInstalledApp, 'permissions'>, hasChat: boolean): TStorageScope[] =>
  app.permissions.includes('storage') ? INSPECT_SCOPES.filter((s) => s !== 'chat' || hasChat) : [];

/** The wall the machine could not make concerns an app with a chat part: its agent is the one outside it. */
export const showsUnwalled = (app: Pick<IInstalledApp, 'targets'>, agentWalled: boolean): boolean =>
  !agentWalled && app.targets.includes('chat');

type TKeys =
  | { k: 'loading' }
  | { k: 'ready'; scopes: { scope: TStorageScope; keys: string[] }[] }
  | { k: 'failed'; text: string };

const Keys = ({
  machine,
  app,
  view,
  target,
  conversation,
}: {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  target: TTarget;
  conversation: ConvId | null;
}) => {
  const [state, setState] = useState<TKeys>({ k: 'loading' });
  const [again, setAgain] = useState(0);
  const scopes = scopesFor(app, conversation !== null);
  const wanted = scopes.join(',');
  useEffect(() => {
    if (wanted === '') return;
    let live = true;
    // The inspector reads through a session of its own, which the runtime ends when it is unused.
    createViewSession(machine, { app: app.id, view: view.id, target, conversation })
      .then(async ({ session }) => {
        const found = await Promise.all(
          wanted.split(',').map(async (scope) => ({
            scope: scope as TStorageScope,
            keys: (await callStorage(machine, {
              session,
              op: 'keys',
              scope: scope as TStorageScope,
              key: '-',
            })) as string[],
          })),
        );
        if (live) setState({ k: 'ready', scopes: found });
      })
      .catch((e: Error) => live && setState({ k: 'failed', text: e.message }));
    return () => {
      live = false;
    };
  }, [machine, app.id, view.id, target, conversation, wanted, again]);

  if (wanted === '') return <p className="hx-insp__none">{t('next.inspector.noStorage')}</p>;
  if (state.k === 'loading') return <p className="hx-insp__none">{t('next.inspector.loading')}</p>;
  if (state.k === 'failed') return <p className="hx-insp__none">{state.text}</p>;
  return (
    <>
      {state.scopes.map(({ scope, keys }) => (
        <div key={scope} className="hx-insp__scope">
          <span className="hx-insp__scope-name">{scope}</span>
          <span className="hx-insp__scope-keys">{keys.length === 0 ? '—' : keys.join(', ')}</span>
        </div>
      ))}
      <button
        type="button"
        className="hx-insp__link"
        onClick={() => {
          setState({ k: 'loading' });
          setAgain((n) => n + 1);
        }}
      >
        {t('next.inspector.refresh')}
      </button>
    </>
  );
};

/** null: not read yet, which is not the same as nothing decided. */
type TDecisions = IAppDecision[] | null | 'failed';

const Decisions = ({
  machine,
  app,
  conversation,
}: {
  machine: MachineId;
  app: string;
  conversation: ConvId;
}) => {
  // The answer is kept with the chat it is for: another chat reads as not read yet, with no reset inside the effect.
  const key = `${machine}|${app}|${conversation}`;
  const [read, setRead] = useState<{ key: string; list: TDecisions }>({ key: '', list: null });
  useEffect(() => {
    let live = true;
    loadAppDecisions(machine, app, conversation)
      .then((found) => live && setRead({ key, list: found }))
      .catch(() => live && setRead({ key, list: 'failed' }));
    return () => {
      live = false;
    };
  }, [key, machine, app, conversation]);
  const list: TDecisions = read.key === key ? read.list : null;
  if (list === null) return <p className="hx-insp__none">{t('next.inspector.loading')}</p>;
  if (list === 'failed') return <p className="hx-insp__none">{t('next.inspector.decisionsFailed')}</p>;
  if (list.length === 0) return <p className="hx-insp__none">{t('next.inspector.noDecisions')}</p>;
  return (
    <table className="hx-insp__calls">
      <tbody>
        {list.map((e, i) => (
          <tr key={`${e.at}-${i}`} data-ok={e.decision !== 'deny'}>
            <td>{clock(e.at)}</td>
            <td>{e.tool}</td>
            <td>{e.rule || '—'}</td>
            <td>{t(`next.inspector.decision.${e.decision}`)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
};

/** What a journal line says in words; a line this page does not know says its code. */
export const journalText = (e: Pick<IJournalEntry, 'what' | 'detail'>): string => {
  const key = `next.journal.what.${e.what}`;
  const words = t(key);
  const said = words === key ? e.what : words;
  return e.detail ? `${said}: ${e.detail}` : said;
};

/** The deprecated methods the view called, each once, with what replaces them (the bridge logs them as it runs them). */
export const deprecatedOf = (calls: readonly IBridgeLogEntry[]): string[] => [
  ...new Set(
    calls.filter((c) => c.message?.startsWith('deprecated:')).map((c) => `${c.method} — ${c.message}`),
  ),
];

type TJournal = { k: 'loading' } | { k: 'ready'; list: IJournalEntry[] } | { k: 'failed'; text: string };

/** The app's journal: what it did of a high or critical class and every change of what it may do. */
export const Journal = ({ machine, app }: { machine: MachineId; app: string }) => {
  const [state, setState] = useState<TJournal>({ k: 'loading' });
  useEffect(() => {
    let live = true;
    loadAppJournal(machine, app).then(
      (list) => live && setState({ k: 'ready', list }),
      (e: Error) => live && setState({ k: 'failed', text: e.message }),
    );
    return () => {
      live = false;
    };
  }, [machine, app]);
  if (state.k === 'loading') return <p className="hx-insp__none">{t('next.inspector.loading')}</p>;
  if (state.k === 'failed') return <p className="hx-insp__none">{state.text}</p>;
  if (state.list.length === 0) return <p className="hx-insp__none">{t('next.journal.none')}</p>;
  return (
    <ul className="hx-journal">
      {state.list.map((e, i) => (
        <li key={`${e.at}-${i}`} data-ok={e.ok}>
          <time dateTime={new Date(e.at).toISOString()}>{clock(e.at)}</time>
          <span>{journalText(e)}</span>
        </li>
      ))}
    </ul>
  );
};

export interface IAppInspectorProps {
  machine: MachineId;
  app: IInstalledApp;
  /** The view on screen; its calls and storage are what the debug part shows. */
  view: IAppView;
  target: TTarget;
  conversation: ConvId | null;
  /** The debug part (calls, storage, reload) is for a dev install; the rights are for anyone. */
  debug: boolean;
  onReload: () => void;
  /** The machine runs the agents of chats with apps inside the wall around the apps (installed apps' `agentWalled`). */
  agentWalled: boolean;
}

/** The size of instructions in tokens, estimated as bytes / 4 (the runtime's apps.EstimateTokens). */
export const instructionTokens = (bytes: number): number => Math.ceil(bytes / 4);

const AppInspector = ({
  machine,
  app,
  view,
  target,
  conversation,
  debug,
  onReload,
  agentWalled,
}: IAppInspectorProps) => {
  const key = logKey(app.id, conversation);
  const calls = useBridgeLog(key);
  const reload = useCallback(() => {
    clearLog(key);
    onReload();
  }, [key, onReload]);
  const shown = [...calls].reverse().slice(0, 100);
  const deprecated = deprecatedOf(calls);
  return (
    <section className="hx-insp" aria-label={t('next.inspector.title')}>
      <div className="hx-insp__sec">
        <h3 className="hx-insp__h">{t('next.inspector.about')}</h3>
        <p className="hx-insp__text">{app.description}</p>
        <div className="hx-insp__facts">
          <span>{app.id}</span>
          <span>{app.version}</span>
          <span>{t(`next.inspector.trust.${app.trust}`)}</span>
          {!!app.instructionsBytes && (
            <span>{t('next.inspector.tokens', { n: instructionTokens(app.instructionsBytes) })}</span>
          )}
        </div>
        {app.always && <p className="hx-insp__text">{t('next.inspector.always')}</p>}
        {app.command && (
          <p className="hx-insp__text">{t('next.inspector.command', { command: app.command })}</p>
        )}
        {showsUnwalled(app, agentWalled) && <p className="hx-insp__warn">{t('next.inspector.unwalled')}</p>}
      </div>
      <AppSetup machine={machine} app={app} />
      {conversation !== null && app.targets.includes('chat') && (
        <div className="hx-insp__sec">
          <h3 className="hx-insp__h">{t('next.inspector.decisions')}</h3>
          <Decisions machine={machine} app={app.id} conversation={conversation} />
        </div>
      )}
      {deprecated.length > 0 && (
        <div className="hx-insp__sec">
          <h3 className="hx-insp__h">{t('next.inspector.deprecated')}</h3>
          {deprecated.map((line) => (
            <p key={line} className="hx-insp__warn">
              {line}
            </p>
          ))}
        </div>
      )}
      <div className="hx-insp__sec">
        <h3 className="hx-insp__h">{t('next.journal.title')}</h3>
        <Journal machine={machine} app={app.id} />
      </div>
      <div className="hx-insp__sec">
        <h3 className="hx-insp__h">{t('next.consent.access')}</h3>
        {app.access ? (
          <AccessList access={app.access} />
        ) : app.permissions.length === 0 ? (
          <p className="hx-insp__none">{t('next.inspector.noRights')}</p>
        ) : (
          <ul className="hx-insp__chips">
            {app.permissions.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        )}
      </div>
      {debug && (
        <>
          <div className="hx-insp__sec">
            <div className="hx-insp__head">
              <h3 className="hx-insp__h">{t('next.inspector.calls')}</h3>
              <Button size="sm" variant="ghost" icon="refresh-cw" onClick={reload}>
                {t('next.inspector.reload')}
              </Button>
            </div>
            {shown.length === 0 ? (
              <p className="hx-insp__none">{t('next.inspector.noCalls')}</p>
            ) : (
              <table className="hx-insp__calls">
                <tbody>
                  {shown.map((e, i) => (
                    <tr key={`${e.at}-${i}`} data-ok={e.ok}>
                      <td>{clock(e.at)}</td>
                      <td>{e.method}</td>
                      <td>{e.permission || '—'}</td>
                      <td>
                        {resultOf(e)}
                        {e.message && <span className="hx-insp__msg">{e.message}</span>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
          <div className="hx-insp__sec">
            <h3 className="hx-insp__h">{t('next.inspector.storage')}</h3>
            <Keys machine={machine} app={app} view={view} target={target} conversation={conversation} />
          </div>
        </>
      )}
    </section>
  );
};

export default AppInspector;
