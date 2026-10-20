import { t } from '@/lib/i18n';
import { useEffect, useState, type MouseEvent } from 'react';
import dayjs from 'dayjs';
import { CheckCircle2, CircleDashed, Clock, Loader2, MinusCircle, PlayCircle, XCircle } from 'lucide-react';
import SiteIcon from '@/components/agents/site-icon';
import IssueKeyText from '@/components/timeline/issue-key-text';
import { appFetch } from '@/lib/app-path';
import { cn } from '@/lib/utils';
import { isPlainClick } from '@/lib/image-link';
import type { IGitLabMr, IGitLabPush } from '@/types/server/hl-gitlab';
import type { IPipeline, TPipelineStatus } from '@/lib/hl-gitlab-ref';

const LIVE_POLL_MS = 15_000;
const IDLE_POLL_MS = 60_000;
const LIVE: TPipelineStatus[] = ['running', 'pending', 'created'];

const PIPELINE: Record<TPipelineStatus, { icon: typeof CheckCircle2; label: string }> = {
  running: { icon: Loader2, label: t('inspector.gitlabTab.ciRunning') },
  pending: { icon: Clock, label: t('inspector.gitlabTab.ciPending') },
  created: { icon: Clock, label: t('inspector.gitlabTab.ciCreated') },
  success: { icon: CheckCircle2, label: t('inspector.gitlabTab.ciSuccess') },
  failed: { icon: XCircle, label: t('inspector.gitlabTab.ciFailed') },
  canceled: { icon: MinusCircle, label: t('inspector.gitlabTab.ciCanceled') },
  skipped: { icon: MinusCircle, label: t('inspector.gitlabTab.ciSkipped') },
  manual: { icon: PlayCircle, label: t('inspector.gitlabTab.ciManual') },
  other: { icon: CircleDashed, label: 'CI' },
};

export const PipelineBadge = ({ pipeline }: { pipeline: IPipeline | null }) => {
  if (!pipeline)
    return (
      <span className="tw-ci tw-ci-none" title={t('inspector.gitlabTab.noPipeline')}>
        <CircleDashed />
      </span>
    );
  const { icon: Icon, label } = PIPELINE[pipeline.status];
  return (
    <a
      className={cn('tw-ci', `tw-ci-${pipeline.status}`)}
      href={pipeline.url}
      target="_blank"
      rel="noopener noreferrer"
      title={t('inspector.gitlabTab.openPipeline', { label })}
      aria-label={label}
    >
      <Icon className={cn(pipeline.status === 'running' && 'tw-inline-spin')} />
    </a>
  );
};

const repoName = (project: string) => project.split('/').at(-1);
const MR_STATE_LABEL: Record<string, string> = {
  opened: t('inspector.gitlabTab.mrOpened'),
  merged: t('inspector.gitlabTab.mrMerged'),
  closed: t('inspector.gitlabTab.mrClosed'),
  locked: t('inspector.gitlabTab.mrLocked'),
};

const MrRow = ({ mr, onOpen }: { mr: IGitLabMr; onOpen?: (url: string, title: string) => void }) => {
  const openHere = (event: MouseEvent<HTMLAnchorElement>) => {
    if (!onOpen || !isPlainClick(event.nativeEvent)) return;
    event.preventDefault();
    onOpen(mr.url, `${repoName(mr.project)} !${mr.iid}`);
  };
  return (
    <li className={cn('tw-gl-row', `tw-gl-mr-${mr.state}`)}>
      <span className="tw-gl-icon" title={`MR ${MR_STATE_LABEL[mr.state] ?? mr.state}`}>
        <SiteIcon href={mr.url} />
        <i className="tw-gl-dot" />
      </span>
      <span className="tw-gl-text">
        <a
          className="tw-gl-ref"
          href={mr.url}
          target="_blank"
          rel="noopener noreferrer"
          title={`${mr.project} !${mr.iid} — ${MR_STATE_LABEL[mr.state] ?? mr.state}`}
          onClick={openHere}
        >
          {repoName(mr.project)} !{mr.iid}
          {mr.draft && <small>{t('inspector.gitlabTab.draft')}</small>}
        </a>
        <span className="tw-gl-title">
          <IssueKeyText text={mr.title} />
        </span>
      </span>
      <PipelineBadge pipeline={mr.pipeline} />
    </li>
  );
};

const PushRow = ({ push }: { push: IGitLabPush }) => (
  <li className="tw-gl-row">
    <span className="tw-gl-icon" title={t('inspector.gitlabTab.branchInGitlab')}>
      <SiteIcon href={`https://${push.host}/${push.project}`} />
    </span>
    <span className="tw-gl-text">
      <a
        className="tw-gl-ref"
        href={`https://${push.host}/${push.project}/-/tree/${encodeURIComponent(push.branch)}`}
        target="_blank"
        rel="noopener noreferrer"
        title={push.project}
      >
        {repoName(push.project)} <code>{push.branch}</code>
      </a>
      <span className="tw-gl-meta">
        {push.pushedAt ? (
          <span title={push.commitTitle ?? ''}>
            {t('inspector.gitlabTab.pushedAgo', { ago: dayjs(push.pushedAt).fromNow() })}
          </span>
        ) : (
          <span>{t('inspector.gitlabTab.noBranch')}</span>
        )}
        {push.unpushed > 0 && (
          <span className="tw-gl-unpushed" title={t('inspector.gitlabTab.unpushedHint')}>
            {t('inspector.gitlabTab.unpushed', { count: push.unpushed })}
          </span>
        )}
      </span>
    </span>
    <PipelineBadge pipeline={push.pipeline} />
  </li>
);

interface IGitLabTabProps {
  paths: string[];
  jsonlPath: string | null;
  mentioned: string[];
  onCount?: (count: number, live: boolean) => void;
  /** Opens a merge request as a tab of the right panel; without it the link goes to GitLab. */
  onOpenMr?: (url: string, title: string) => void;
}

/** Merge requests and pushed branches of the session, each with its latest pipeline. */
const GitLabTab = ({ paths, jsonlPath, mentioned, onCount, onOpenMr }: IGitLabTabProps) => {
  const [data, setData] = useState<{ pushes: IGitLabPush[]; mrs: IGitLabMr[] } | null>(null);
  const request = JSON.stringify({ paths, jsonlPath, mrs: mentioned });
  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = async () => {
      let live = false;
      try {
        const response = await appFetch('/api/platform/gitlab', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: request,
        });
        if (response.ok) {
          const next = (await response.json()) as { pushes: IGitLabPush[]; mrs: IGitLabMr[] };
          if (cancelled) return;
          setData(next);
          live = [...next.pushes, ...next.mrs].some(
            (item) => item.pipeline && LIVE.includes(item.pipeline.status),
          );
        }
      } catch {}
      if (!cancelled) timer = setTimeout(load, live ? LIVE_POLL_MS : IDLE_POLL_MS);
    };
    void load();
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [request]);

  const live =
    !!data &&
    [...data.pushes, ...data.mrs].some((item) => item.pipeline && LIVE.includes(item.pipeline.status));
  const count = data ? data.pushes.length + data.mrs.length : 0;
  useEffect(() => {
    onCount?.(count, live);
  }, [count, live, onCount]);

  if (!data)
    return (
      <p className="tw-inspector-muted">
        <Loader2 className="tw-inline-spin" /> {t('inspector.gitlabTab.loading')}
      </p>
    );
  if (count === 0) return <p className="tw-inspector-muted">{t('inspector.gitlabTab.empty')}</p>;
  return (
    <div className="tw-gl">
      {data.mrs.length > 0 && (
        <section>
          <h3 className="tw-inspector-heading">
            Merge requests <span>{data.mrs.length}</span>
          </h3>
          <ul className="tw-gl-list">
            {data.mrs.map((mr) => (
              <MrRow key={`${mr.project}!${mr.iid}`} mr={mr} onOpen={onOpenMr} />
            ))}
          </ul>
        </section>
      )}
      {data.pushes.length > 0 && (
        <section>
          <h3 className="tw-inspector-heading">
            {t('inspector.gitlabTab.pushes')} <span>{data.pushes.length}</span>
          </h3>
          <ul className="tw-gl-list">
            {data.pushes.map((push) => (
              <PushRow key={`${push.project}@${push.branch}`} push={push} />
            ))}
          </ul>
        </section>
      )}
    </div>
  );
};

export default GitLabTab;
