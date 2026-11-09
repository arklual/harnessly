import { memo } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import dayjs from 'dayjs';
import { ArrowUpRight, CornerLeftUp, ExternalLink } from 'lucide-react';
import MessageLink from '@/components/timeline/message-link';
import { AssigneeAvatar, IssueState } from '@/components/agents/link-meta-parts';
import useTask from '@/hooks/use-task';
import useConnPage from '@/hooks/use-conn-page';
import { parseConnTarget } from '@/lib/conn-target';
import { issueTone, ISSUE_TONE_LABEL } from '@/lib/link-meta';
import remarkIssueKeys from '@/lib/remark-issue-keys';
import { cleanTaskText, isTaskKey, type ITask, type ITaskComment, type TTaskError } from '@/lib/hl-task';
import { issueUrl } from '@/lib/tracker-keys';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

interface ITaskTabProps {
  /** The key of the task, `PROJ-123`; anything else is not a task of the tracker. */
  taskKey: string | null;
  onOpenTask: (key: string, title: string | null) => void;
}

const TASK_PAGE_MAX = 60 * 1024;
const REMARK_PLUGINS = [remarkGfm, remarkIssueKeys];
/** The text comes from the tracker: no raw HTML (react-markdown never renders it), no pictures that would load from anywhere. */
const MARKDOWN_COMPONENTS = { a: MessageLink, img: () => null } as Components;

const ERROR_TEXT: Record<TTaskError, string> = {
  'not-found': t('pane.taskTab.errorNotFound'),
  'no-token': t('pane.taskTab.errorNoToken'),
  tracker: t('pane.taskTab.errorTracker'),
};

const Markdown = ({ text }: { text: string }) => (
  <div className="tw-task-md">
    <ReactMarkdown remarkPlugins={REMARK_PLUGINS} components={MARKDOWN_COMPONENTS}>
      {text}
    </ReactMarkdown>
  </div>
);

const Comment = ({ comment }: { comment: ITaskComment }) => (
  <li className="tw-task-comment">
    <header>
      <b>{comment.author}</b>
      {comment.at !== null && (
        <time
          dateTime={new Date(comment.at).toISOString()}
          title={dayjs(comment.at).format('DD.MM.YYYY HH:mm')}
        >
          {dayjs(comment.at).fromNow()}
        </time>
      )}
    </header>
    <Markdown text={comment.text} />
  </li>
);

const Skeleton = () => (
  <div className="tw-task" aria-busy="true" aria-label={t('pane.taskTab.loading')}>
    <span className="tw-skeleton tw-task-skeleton-key" />
    <span className="tw-skeleton tw-task-skeleton-title" />
    <span className="tw-skeleton tw-task-skeleton-chips" />
    <span className="tw-skeleton tw-task-skeleton-text" />
    <span className="tw-skeleton tw-task-skeleton-text" />
    <span className="tw-skeleton tw-task-skeleton-text" />
  </div>
);

const TaskView = ({ task, onOpenTask }: { task: ITask; onOpenTask: ITaskTabProps['onOpenTask'] }) => {
  return (
    <article className="tw-task">
      <header className="tw-task-head">
        <code className="tw-task-key">{task.key}</code>
        <a
          className="tw-task-tracker"
          href={issueUrl(task.key)}
          target="_blank"
          rel="noopener noreferrer"
          title={t('pane.taskTab.openInTrackerHint')}
        >
          <ExternalLink /> {t('pane.taskTab.openInTracker')}
        </a>
      </header>
      <h3 className="tw-task-title">{task.title}</h3>
      <ul className="tw-task-chips" aria-label={t('pane.taskTab.properties')}>
        {task.type && (
          <li>
            <small>{t('pane.taskTab.type')}</small>
            {task.type}
          </li>
        )}
        <li>
          <small>{t('pane.taskTab.state')}</small>
          <IssueState
            meta={{
              kind: 'issue',
              key: task.key,
              title: task.title,
              state: task.state,
              resolved: task.resolved,
              assignee: null,
            }}
            withLabel
          />
        </li>
        {task.priority && (
          <li>
            <small>{t('pane.taskTab.priority')}</small>
            {task.priority}
          </li>
        )}
      </ul>
      <p className="tw-task-meta">
        {task.assignee ? (
          <span className="tw-task-person">
            <AssigneeAvatar assignee={task.assignee} />
            {task.assignee.name}
          </span>
        ) : (
          <span className="tw-task-muted">{t('pane.taskTab.unassigned')}</span>
        )}
        {task.updated !== null && (
          <span className="tw-task-muted" title={dayjs(task.updated).format('DD.MM.YYYY HH:mm')}>
            {t('pane.taskTab.updated', { time: dayjs(task.updated).fromNow() })}
          </span>
        )}
      </p>
      {task.parent && (
        <button
          type="button"
          className="tw-task-parent"
          onClick={() => onOpenTask(task.parent!.key, task.parent!.key)}
          title={t('pane.taskTab.open', { key: task.parent.key })}
        >
          <CornerLeftUp />
          <b>{task.parent.key}</b>
          <span>{task.parent.title}</span>
        </button>
      )}
      {task.children.length > 0 && (
        <section className="tw-task-section" aria-label={t('pane.taskTab.subtasks')}>
          <h4 className="tw-inspector-heading">
            {t('pane.taskTab.subtasks')} <span>{task.children.length}</span>
          </h4>
          <ul className="tw-task-children">
            {task.children.map((child) => (
              <li key={child.key}>
                <button
                  type="button"
                  className="tw-task-child"
                  onClick={() => onOpenTask(child.key, child.key)}
                  title={t('pane.taskTab.open', { key: child.key })}
                >
                  <span
                    className={cn('tw-link-state', `tw-issue-${issueTone(child.state, child.resolved)}`)}
                    aria-hidden="true"
                  >
                    <i />
                  </span>
                  <b>{child.key}</b>
                  <span>{child.title}</span>
                  <small>{child.state ?? ISSUE_TONE_LABEL[issueTone(child.state, child.resolved)]}</small>
                  <ArrowUpRight aria-hidden="true" />
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
      <section className="tw-task-section" aria-label={t('pane.taskTab.description')}>
        <h4 className="tw-inspector-heading">{t('pane.taskTab.description')}</h4>
        {task.description ? (
          <Markdown text={task.description} />
        ) : (
          <p className="tw-task-muted">{t('pane.taskTab.noDescription')}</p>
        )}
      </section>
      {task.comments.length > 0 && (
        <section className="tw-task-section" aria-label={t('pane.taskTab.comments')}>
          <h4 className="tw-inspector-heading">
            {t('pane.taskTab.comments')} <span>{task.comments.length}</span>
          </h4>
          <ul className="tw-task-comments">
            {task.comments.map((comment, index) => (
              <Comment key={`${comment.at}-${index}`} comment={comment} />
            ))}
          </ul>
        </section>
      )}
    </article>
  );
};

const PAGE_ERROR: Record<'not-found' | 'denied' | 'failed', string> = {
  'not-found': t('pane.taskTab.pageNotFound'),
  denied: t('pane.taskTab.pageDenied'),
  failed: t('pane.taskTab.pageFailed'),
};

/** A wiki page of a connection, read in the pane: Markdown with no raw HTML and no pictures, and a way to the original. */
const PageTab = ({ conn, id }: { conn: string; id: string }) => {
  const [load, reload] = useConnPage(conn, id);
  if (load.status === 'loading') return <Skeleton />;
  if (load.status === 'error') {
    return (
      <div className="tw-task tw-task-error" role="alert">
        <code className="tw-task-key">{id}</code>
        <p>{PAGE_ERROR[load.error]}</p>
        {load.error !== 'not-found' && (
          <div className="tw-task-error-actions">
            <button type="button" className="tw-task-retry" onClick={reload}>
              {t('pane.taskTab.retry')}
            </button>
          </div>
        )}
      </div>
    );
  }
  const { page } = load;
  const original = /^https?:\/\//i.test(page.url) ? page.url : null;
  return (
    <article className="tw-task">
      <h2 className="tw-task-title">{page.title || id}</h2>
      {original && (
        <a className="tw-task-tracker" href={original} target="_blank" rel="noopener noreferrer">
          <ExternalLink /> {t('pane.taskTab.openOriginal')}
        </a>
      )}
      <Markdown text={cleanTaskText(page.body, TASK_PAGE_MAX)} />
    </article>
  );
};

/** A task of the tracker as a tab of the right panel, so a click on its chip does not leave the conversation. A target
 *  that names a connection (`conn:<id>:<issue|page>:<key>`) is read through that connection. */
const TaskTab = ({ taskKey, onOpenTask }: ITaskTabProps) => {
  const connTarget = parseConnTarget(taskKey);
  const valid = connTarget
    ? connTarget.kind === 'issue'
      ? connTarget.key
      : null
    : isTaskKey(taskKey)
      ? taskKey
      : null;
  const [load, reload] = useTask(valid, connTarget?.conn ?? null);
  if (connTarget?.kind === 'page') return <PageTab conn={connTarget.conn} id={connTarget.key} />;
  if (!valid) return <p className="tw-rpane-muted">{t('pane.taskTab.notTaskKey')}</p>;
  if (load.status === 'loading') return <Skeleton />;
  if (load.status === 'error') {
    return (
      <div className="tw-task tw-task-error" role="alert">
        <code className="tw-task-key">{valid}</code>
        <p>{ERROR_TEXT[load.error]}</p>
        <div className="tw-task-error-actions">
          {load.error !== 'not-found' && (
            <button type="button" className="tw-task-retry" onClick={reload}>
              {t('pane.taskTab.retry')}
            </button>
          )}
          {!connTarget && (
            <a className="tw-task-tracker" href={issueUrl(valid)} target="_blank" rel="noopener noreferrer">
              <ExternalLink /> {t('pane.taskTab.openInTracker')}
            </a>
          )}
        </div>
      </div>
    );
  }
  return <TaskView task={load.task} onOpenTask={onOpenTask} />;
};

export default memo(TaskTab);
