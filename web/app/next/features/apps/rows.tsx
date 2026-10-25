// The rows of the Apps screen: an epic with its subtasks, a task, a merge request. A row is picked with its check (or
// the key); the bar under the list takes the picked ones into a new chat, and so does the row's own «To chat».
import { useState, type KeyboardEvent, type ReactNode } from 'react';
import {
  agoText,
  daysStill,
  isDone,
  isStale,
  matchingChildren,
  PIPELINE_ICON,
  PIPELINE_TONE,
  priorityRank,
  progressShare,
  rowTag,
  stateText,
} from '@/next/features/apps/model';
import { plural, t } from '@/next/i18n';
import type { AppRow } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { useSeen } from '@/next/ui/presence';

/** Rows of a list take at most this many places of the entrance cascade (--j), so a long list does not wait. */
const CASCADE_PLACES = 8;
/** A row's labels beyond this many are folded into «+n». */
const LABELS_SHOWN = 3;
/** The priority the tracker calls the usual one: it is not worth a word on a row. */
const USUAL_PRIORITY = 'Normal';

export interface IRowProps {
  row: AppRow;
  place: number;
  on: boolean;
  now: number;
  staleDays: number;
  onToggle: (key: string) => void;
  onChat: (row: AppRow) => void;
}

const place = (index: number) => (element: HTMLElement | null) =>
  element?.style.setProperty('--j', String(Math.min(index, CASCADE_PLACES)));

const Labels = ({ labels }: { labels: readonly string[] }) => {
  if (labels.length === 0) return null;
  return (
    <span className="hx-hubchips">
      {labels.slice(0, LABELS_SHOWN).map((label) => (
        <span key={label} className="hx-hubchip">
          {label}
        </span>
      ))}
      {labels.length > LABELS_SHOWN && (
        <span className="hx-hubchip" title={labels.slice(LABELS_SHOWN).join(', ')}>
          +{labels.length - LABELS_SHOWN}
        </span>
      )}
    </span>
  );
};

const Dot = () => (
  <span className="hx-hubdot" aria-hidden="true">
    ·
  </span>
);

interface IShellProps extends IRowProps {
  /** The lines under the title. */
  children: ReactNode;
  className?: string;
}

/** What every row is: a check, the id and the title, the status tag and «To chat». */
const Shell = ({ row, place: at, on, onToggle, onChat, children, className }: IShellProps) => {
  const tag = rowTag(row);
  const toggle = () => onToggle(row.key);
  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget || (event.key !== 'Enter' && event.key !== ' ')) return;
    event.preventDefault();
    toggle();
  };
  return (
    <div
      className={className ? `hx-hubrow ${className}` : 'hx-hubrow'}
      role="checkbox"
      tabIndex={0}
      aria-checked={on}
      data-done={isDone(row) || undefined}
      ref={place(at)}
      onClick={toggle}
      onKeyDown={onKey}
    >
      <span className="hx-hubrow__check" aria-hidden="true">
        <Icon name="check" size="sm" />
      </span>
      <span className="hx-hubrow__txt">
        <span className="hx-hubrow__name">
          <span className="hx-hubrow__id">{row.id}</span>
          <span className="hx-hubrow__t">{row.title}</span>
        </span>
        {children}
      </span>
      {tag && (
        <span className="hx-hubrow__tag" data-tone={isDone(row) ? undefined : (row.tone ?? undefined)}>
          {tag}
        </span>
      )}
      <button
        type="button"
        className="hx-hubrow__go"
        aria-label={t('next.apps.chatAria')}
        onClick={(event) => {
          event.stopPropagation();
          onChat(row);
        }}
      >
        <Icon name="message-square-plus" size="sm" />
        <span>{t('next.apps.chat')}</span>
      </button>
    </div>
  );
};

/** «9 days without a move», in the warning colour: only for work that stands still. */
const Still = ({ row, now, staleDays }: { row: AppRow; now: number; staleDays: number }) => {
  const stale = isStale(row, now, staleDays);
  return (
    <span className="hx-hubrow__when" data-stale={stale || undefined}>
      {stale ? plural(daysStill(row, now), 'next.apps.still') : agoText(row.updatedAt, now)}
    </span>
  );
};

const unusual = (priority: string | null): string | null =>
  priority && priority !== USUAL_PRIORITY && priorityRank(priority) < priorityRank(USUAL_PRIORITY)
    ? priority
    : null;

/** Under a task's title: its epic, its kind, a priority that is not the usual one, the tags, when it moved. */
export const TaskRow = (props: IRowProps) => {
  const { row, now, staleDays } = props;
  const urgent = unusual(row.priority);
  return (
    <Shell {...props}>
      <span className="hx-hubrow__meta">
        {row.parent && (
          <>
            <span className="hx-hubrow__epic" title={row.parent.title}>
              <Icon name="layers" size="sm" />
              {row.parent.id}
            </span>
            <Dot />
          </>
        )}
        {row.kind && <span>{row.kind}</span>}
        {urgent && (
          <>
            {row.kind && <Dot />}
            <span className="hx-hubrow__urgent">{urgent}</span>
          </>
        )}
        {(row.kind || urgent) && <Dot />}
        <Still row={row} now={now} staleDays={staleDays} />
      </span>
      <Labels labels={row.labels} />
    </Shell>
  );
};

interface IEpicProps extends IRowProps {
  /** The words of the search: the subtasks shown are the ones they find. */
  words: readonly string[];
  /** Whether subtasks open by themselves (a search that found some, or the person's choice). */
  openByDefault: boolean;
}

/** An epic: its progress with a fold that opens the subtasks under it. */
export const EpicRow = (props: IEpicProps) => {
  const { row, now, staleDays, words, openByDefault } = props;
  const [chosen, setChosen] = useState<boolean | null>(null);
  const kids = matchingChildren(row, words);
  const open = (chosen ?? (openByDefault || words.length > 0)) && kids.length > 0;
  const seen = useSeen(open);
  const share = progressShare(row);
  const urgent = unusual(row.priority);
  return (
    <div className="hx-hubepic" data-open={open || undefined}>
      <Shell {...props} className="hx-hubrow--epic">
        <span className="hx-hubrow__meta">
          <button
            type="button"
            className="hx-hubprog"
            aria-expanded={open}
            aria-label={t('next.apps.kids')}
            disabled={row.children.length === 0}
            onClick={(event) => {
              event.stopPropagation();
              setChosen(!open);
            }}
            onKeyDown={(event) => event.stopPropagation()}
          >
            <span className="hx-hubprog__bar" aria-hidden="true">
              <span
                className="hx-hubprog__fill"
                data-full={share === 1 || undefined}
                ref={(element) => element?.style.setProperty('--p', String(share))}
              />
            </span>
            <span className="hx-hubprog__text">
              {row.progress && row.progress.total > 0
                ? t('next.apps.progressShort', { done: row.progress.done, total: row.progress.total })
                : t('next.apps.noTasks')}
            </span>
            {row.children.length > 0 && <Icon name="chevron-down" size="sm" className="hx-hubprog__chev" />}
          </button>
          {urgent && (
            <>
              <Dot />
              <span className="hx-hubrow__urgent">{urgent}</span>
            </>
          )}
          <Dot />
          <Still row={row} now={now} staleDays={staleDays} />
        </span>
        <Labels labels={row.labels} />
      </Shell>
      {(open || seen) && (
        <div className="hx-hubkids" data-open={open}>
          <ul className="hx-hubkids__in">
            {kids.map((kid) => (
              <li key={kid.id}>
                <a className="hx-hubkid" href={kid.url} target="_blank" rel="noreferrer" data-done={kid.done}>
                  <Icon name={kid.done ? 'circle-check' : 'circle-dashed'} size="sm" />
                  <span className="hx-hubkid__id">{kid.id}</span>
                  <span className="hx-hubkid__t">{kid.title}</span>
                  {kid.person && <span className="hx-hubkid__who">{kid.person}</span>}
                  {kid.state && <span className="hx-hubkid__state">{stateText(kid.state)}</span>}
                </a>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};

const Signal = ({
  icon,
  tone,
  children,
  title,
}: {
  icon: string;
  tone?: string;
  children: ReactNode;
  title?: string;
}) => (
  <span className="hx-hubsig" data-tone={tone} title={title}>
    <Icon name={icon} size="sm" />
    {children}
  </span>
);

/** Under a merge request's title: where it goes and who has it; and what is known of it — the pipeline, the approvals,
 *  conflicts, open discussions, whether it can be merged. */
export const MrRow = (props: IRowProps) => {
  const { row, now, staleDays } = props;
  const mr = row.mr;
  const open = row.state === 'opened';
  const pipeline = mr?.pipeline ?? null;
  const people = [row.author, ...row.assignees.filter((name) => name !== row.author)].filter(
    (name): name is string => !!name,
  );
  return (
    <Shell {...props}>
      <span className="hx-hubrow__meta">
        {row.project && <span className="hx-hubrow__project">{row.project}</span>}
        {mr?.source && row.target && (
          <span className="hx-hubrow__branches" title={`${mr.source} → ${row.target}`}>
            <Icon name="git-branch" size="sm" />
            <span className="hx-hubrow__src">{mr.source}</span>
            <Icon name="arrow-right" size="sm" />
            {row.target}
          </span>
        )}
        {people.length > 0 && (
          <span className="hx-hubrow__people" title={people.join(', ')}>
            {people.slice(0, 2).join(' · ')}
          </span>
        )}
        <Still row={row} now={now} staleDays={staleDays} />
      </span>
      {mr && (
        <span className="hx-hubsigs">
          {open && pipeline && (
            <Signal icon={PIPELINE_ICON[pipeline]} tone={PIPELINE_TONE[pipeline]}>
              {t(`next.apps.pipeline.${pipeline}`)}
            </Signal>
          )}
          {open && mr.approvals && (
            <Signal
              icon="shield-check"
              tone={mr.approvals.given >= mr.approvals.required ? 'good' : undefined}
              title={t('next.apps.approvals')}
            >
              {mr.approvals.given}/{mr.approvals.required}
            </Signal>
          )}
          {open && mr.conflicts && (
            <Signal icon="triangle-alert" tone="bad">
              {t('next.apps.mr.conflicts')}
            </Signal>
          )}
          {open && mr.needsRebase && !mr.conflicts && (
            <Signal icon="git-compare" tone="warn">
              {t('next.apps.mr.rebase')}
            </Signal>
          )}
          {open && mr.unresolved && (
            <Signal icon="message-square-text" tone="warn">
              {t('next.apps.mr.threads')}
            </Signal>
          )}
          {open && mr.ready && !mr.draft && (
            <Signal icon="git-merge" tone="good">
              {t('next.apps.mr.ready')}
            </Signal>
          )}
          {mr.comments > 0 && (
            <Signal icon="message-square" title={t('next.apps.comments')}>
              {mr.comments}
            </Signal>
          )}
          <Labels labels={row.labels} />
        </span>
      )}
    </Shell>
  );
};
