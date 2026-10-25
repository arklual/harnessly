// «История доступа», a view of the share dialog: opening it reads the last 100 lines, newest first. The lines wait
// behind a skeleton that shows after 150 ms, and an empty log is not the same thing as one not yet read.
import { useEffect, useState } from 'react';
import { fetchLog, type ILogEntry } from '@/next/features/designly/access/api';
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
]);

const lineOf = (entry: ILogEntry): string => {
  const what = LINES.has(entry.action) ? t(`next.access.log.${entry.action}`) : entry.action;
  return [entry.actor === 'system' ? t('next.access.log.system') : entry.actor, what, entry.subject]
    .filter(Boolean)
    .join(' · ');
};

const when = (iso: string): string =>
  new Date(iso).toLocaleString(undefined, {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });

const AccessLog = ({ project }: { project: string }) => {
  const [entries, setEntries] = useState<ILogEntry[] | null>(null);
  const [failed, setFailed] = useState<string | null>(null);
  const skeleton = useDelayed(entries === null && failed === null);

  useEffect(() => {
    const abort = new AbortController();
    fetchLog(project, abort.signal).then(setEntries, (failure: Error) => {
      if (!abort.signal.aborted) setFailed(failure.message);
    });
    return () => abort.abort();
  }, [project]);

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
              <span>{lineOf(entry)}</span>
              <time dateTime={entry.at}>{when(entry.at)}</time>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};

export default AccessLog;
