// What stands in the place of a view that did not start: the reason in words, and three ways on: try again,
// read the window's log, hand it to the agent. The frame itself is hidden then, so no blank page is left.
import { useState } from 'react';
import { APP_REASON, type IInstalledApp } from '@/next/features/app-views/api';
import { useBridgeLog } from '@/next/features/app-views/bridge-log';
import type { IBridgeLogEntry } from '@/next/features/app-views/bridge';
import { setAppsDebug } from '@/next/features/app-views/debug-mode';
import { FAIL_REASON, type IFailure } from '@/next/features/app-views/frame-health';
import type { ConvId } from '@/next/protocol/v2';
import { draftText, setDraftText } from '@/next/stores/drafts';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

/** How many of the window's log lines the card shows and the agent gets. */
const LOG_LINES = 8;

const waitingKind = (reason: string): string =>
  reason === APP_REASON.consent || reason === APP_REASON.secret || reason === APP_REASON.connection
    ? reason
    : 'other';

/** The reason in the person's words. */
export const failureText = (failure: IFailure): string => {
  const { reason, detail } = failure;
  if (reason === FAIL_REASON.waiting) return t(`next.appfail.reason.waiting.${waitingKind(detail)}`);
  if (reason === FAIL_REASON.server && detail !== '') return t('next.appfail.reason.serverWhy', { detail });
  if (reason === FAIL_REASON.silent || reason === FAIL_REASON.auth || reason === FAIL_REASON.server)
    return t(`next.appfail.reason.${reason}`);
  return t(`next.appfail.reason.${reason}`, { detail });
};

/** One line of the window's log for the card and for the agent. */
export const logLine = (e: IBridgeLogEntry): string =>
  [e.method, e.code, e.message].filter((part) => part !== undefined && part !== '').join(' — ');

/** The note put in the chat field: what failed and the last of the log. */
export const handoffText = (
  app: Pick<IInstalledApp, 'id' | 'name'>,
  failure: IFailure,
  log: readonly IBridgeLogEntry[],
): string => {
  const head = t('next.appfail.handoffText', { name: app.name, id: app.id, reason: failureText(failure) });
  const lines = log
    .filter((e) => !e.ok)
    .slice(-LOG_LINES)
    .map(logLine);
  return lines.length === 0
    ? head
    : `${head}\n${t('next.appfail.handoffLog')}\n${lines.map((l) => `- ${l}`).join('\n')}`;
};

interface IAppFailProps {
  app: IInstalledApp;
  failure: IFailure;
  /** The key of the app's bridge log in this chat. */
  logKey: string;
  conversation: ConvId | null;
  onRetry: () => void;
}

const AppFail = ({ app, failure, logKey, conversation, onRetry }: IAppFailProps) => {
  const [journal, setJournal] = useState(false);
  const [handed, setHanded] = useState(false);
  const log = useBridgeLog(logKey);
  const failed = log.filter((e) => !e.ok).slice(-LOG_LINES);
  const hand = () => {
    if (conversation === null) return;
    const have = draftText(conversation);
    const note = handoffText(app, failure, log);
    setDraftText(conversation, have === '' ? note : `${have}\n\n${note}`);
    setHanded(true);
  };
  return (
    <div className="hx-appfail" role="alert">
      <div className="hx-appfail__head">
        <Icon name={app.icon || 'layout-grid'} />
        <div className="hx-appfail__txt">
          <h3 className="hx-appfail__title">{t('next.appfail.title', { name: app.name })}</h3>
          <p className="hx-appfail__why">{failureText(failure)}</p>
        </div>
      </div>
      {/* HRN-915: when the app waits for permission, its «Разрешить» goes here. */}
      <div className="hx-appfail__acts">
        <Button size="sm" variant="primary" icon="refresh-cw" onClick={onRetry}>
          {t('next.appfail.retry')}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          icon="scroll-text"
          onClick={() => {
            if (!journal) setAppsDebug(true);
            setJournal(!journal);
          }}
        >
          {journal ? t('next.appfail.journalHide') : t('next.appfail.journal')}
        </Button>
        {conversation !== null && (
          <Button size="sm" variant="ghost" icon="send" disabled={handed} onClick={hand}>
            {handed ? t('next.appfail.handedOff') : t('next.appfail.handoff')}
          </Button>
        )}
      </div>
      {journal &&
        (failed.length === 0 ? (
          <p className="hx-appfail__none">{t('next.appfail.journalEmpty')}</p>
        ) : (
          <ul className="hx-appfail__log">
            {failed.map((e, i) => (
              <li key={`${e.at}-${i}`}>{logLine(e)}</li>
            ))}
          </ul>
        ))}
    </div>
  );
};

export default AppFail;
