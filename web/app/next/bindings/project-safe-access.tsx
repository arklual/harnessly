import { useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { isSafeRefusal, loadSafe, type ISecret } from '@/next/features/safe/api';
import { safeReasonText, usedLine } from '@/next/features/settings/safe-section';
import { formatAgoFull } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import LoadFailed from '@/next/ui/load-failed';
import { appBasePath } from '@/platform/config';
import Router from '@/platform/router';
import '@/next/styles/safe.css';
import '@/next/styles/project-access.css';

type TRead =
  | { k: 'ready'; machine: MachineId; project: string; secrets: readonly ISecret[] }
  | { k: 'failed'; machine: MachineId; project: string; reason: string };

/** Project settings only reads grants. Access is edited in the machine's Safe page. */
export const ProjectSafeAccess = ({
  machine,
  project,
  onEdit,
}: {
  machine: MachineId;
  project: string;
  onEdit?: () => void;
}) => {
  const [now] = useState(() => Date.now());
  const [read, setRead] = useState<TRead | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    void loadSafe(machine).then((res) => {
      if (!live) return;
      if (!('ok' in res)) setRead({ k: 'failed', machine, project, reason: pageText(res) });
      else if (isSafeRefusal(res.value))
        setRead({ k: 'failed', machine, project, reason: safeReasonText(res.value.refused) });
      else setRead({ k: 'ready', machine, project, secrets: res.value.secrets });
    });
    return () => {
      live = false;
    };
  }, [machine, project, attempt]);
  const current = read?.machine === machine && read.project === project ? read : null;
  return (
    <fieldset className="hx-project-safe">
      <legend>{t('next.safe.title')}</legend>
      <p className="hx-safe__hint">{t('next.safe.project.note')}</p>
      {current === null ? (
        <p role="status">{t('next.settings.loading')}</p>
      ) : current.k === 'failed' ? (
        <LoadFailed
          title={t('next.load.failed')}
          reason={current.reason}
          onRetry={() => {
            setRead(null);
            setAttempt((n) => n + 1);
          }}
        />
      ) : (
        <ProjectSecrets secrets={current.secrets} project={project} now={now} onEdit={onEdit} />
      )}
    </fieldset>
  );
};

export const ProjectSecrets = ({
  secrets,
  project,
  now,
  onEdit,
}: {
  secrets: readonly ISecret[];
  project: string;
  now: number;
  onEdit?: () => void;
}) => {
  const granted = secrets.filter((secret) => !secret.projects?.length || secret.projects.includes(project));
  if (granted.length === 0) return <p className="hx-safe__hint">{t('next.safe.project.empty')}</p>;
  return (
    <ul className="hx-project-safe__list">
      {granted.map((secret) => {
        const path = `/settings/safe?secret=${encodeURIComponent(secret.name)}`;
        return (
          <li key={secret.name}>
            <div className="hx-project-safe__head">
              <strong className="hx-safe__name">{secret.name}</strong>
              <a
                href={`${appBasePath()}${path}`}
                onClick={(event) => {
                  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
                    return;
                  event.preventDefault();
                  onEdit?.();
                  void Router.push(path);
                }}
              >
                {t('next.safe.editAccess')}
              </a>
            </div>
            <p>
              <strong>{t('next.safe.origins')}:</strong>{' '}
              {secret.origins?.join(', ') || t('next.safe.origins.empty')}
            </p>
            <p>{usedLine(secret, now)}</p>
            {secret.usedBy.length > 0 && (
              <div className="hx-project-safe__uses">
                <strong>{t('next.safe.latestUses')}</strong>
                <ul>
                  {secret.usedBy.map((use, index) => (
                    <li key={`${use.conv}:${use.at}:${index}`}>
                      {use.title || use.conv || t('next.safe.use.agent')}
                      {use.origin && <> · {use.origin}</>} · {formatAgoFull(use.at, now)}
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
};
