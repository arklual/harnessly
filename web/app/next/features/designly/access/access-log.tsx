// «История доступа», a view of the share dialog: opening it reads the last 100 lines, newest first. The lines wait
// behind a skeleton that shows after 150 ms, and an empty log is not the same thing as one not yet read.
import { useEffect, useState } from 'react';
import { fetchLog, type ILogEntry } from '@/next/features/designly/access/api';
import { fetchResourceLog, type TResourceKind } from '@/next/features/designly/access/resource-api';
import { Skeleton, useDelayed } from '@/next/ui/skeleton';
import { t } from '@/next/i18n';

const LINES = new Set([
  'create',
  'claim',
  'team',
  'settings',
  'grant',
  'change',
  'revoke',
  'revoke-all',
  'transfer',
  'request',
  'allow',
  'deny',
  'invite',
  'invite-revoke',
  'invite-accept',
  'expire',
  'team_grant',
  'team_change',
  'team_revoke',
  'share.create',
  'share.update',
  'share.revoke',
  'share.enable',
  'share.reissue',
  'share.comment',
]);

const TEAM = 'team:';

/** Names for what a line names by id: a team (`team:<id>`), a link without sign-in. */
export interface ILogNames {
  teams?: ReadonlyMap<string, string>;
  links?: ReadonlyMap<string, string>;
}

const subjectOf = (entry: ILogEntry, names: ILogNames): string => {
  const subject = entry.subject;
  if (subject.startsWith(TEAM)) {
    const name = names.teams?.get(subject.slice(TEAM.length));
    return name ? t('next.access.teamName', { name }) : t('next.access.log.someTeam');
  }
  const link = names.links?.get(subject);
  if (link !== undefined) return `«${link}»`;
  return subject;
};

const lineOf = (entry: ILogEntry, names: ILogNames): string => {
  const what = LINES.has(entry.action) ? t(`next.access.log.${entry.action}`) : entry.action;
  return [entry.actor === 'system' ? t('next.access.log.system') : entry.actor, what, subjectOf(entry, names)]
    .filter(Boolean)
    .join(' · ');
};

const NO_NAMES: ILogNames = {};

// One formatter for every line: toLocaleString builds a new one per call, and a hundred lines took a frame and more.
let stamp: Intl.DateTimeFormat | null = null;
const when = (iso: string): string => {
  stamp ??= new Intl.DateTimeFormat(undefined, {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
  return stamp.format(new Date(iso));
};

/** Makes the formatter while the dialog stands idle: the first one of a page loads the language's data, and that was
 *  most of the first opening of «История доступа». */
export const warmLog = (): (() => void) => {
  if (typeof window.requestIdleCallback !== 'function') return () => undefined;
  const handle = window.requestIdleCallback(() => void when(new Date().toISOString()), { timeout: 2000 });
  return () => window.cancelIdleCallback(handle);
};

/** `kind` reads the log through the one access API (HRN-675); without it, the design's older route. */
const AccessLog = ({
  project,
  kind,
  names = NO_NAMES,
}: {
  project: string;
  kind?: TResourceKind;
  names?: ILogNames;
}) => {
  const [entries, setEntries] = useState<ILogEntry[] | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const skeleton = useDelayed(entries === null && failed === null);

  useEffect(() => {
    const abort = new AbortController();
    const read = kind
      ? fetchResourceLog({ kind, id: project }, abort.signal)
      : fetchLog(project, abort.signal);
    read.then(setEntries, (failure: Error) => {
      if (!abort.signal.aborted) setFailed(failure.message);
    });
    return () => abort.abort();
  }, [project, kind]);

  return (
    <div className="hx-acc-stack" data-section="log">
      {skeleton && (
        <div className="hx-acc-log" aria-hidden="true">
          <Skeleton className="hx-acc-sk" />
          <Skeleton className="hx-acc-sk" />
          <Skeleton className="hx-acc-sk" />
        </div>
      )}
      {failed && <p className="hx-create__err">{failed}</p>}
      {entries !== null && entries.length === 0 && (
        <p className="hx-acc-note">{t('next.access.log.empty')}</p>
      )}
      {entries !== null && entries.length > 0 && (
        <ul className="hx-acc-log">
          {entries.map((entry) => (
            <li key={entry.id}>
              <span>{lineOf(entry, names)}</span>
              <time dateTime={entry.at}>{when(entry.at)}</time>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

export default AccessLog;
