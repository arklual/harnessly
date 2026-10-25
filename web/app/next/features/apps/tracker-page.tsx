// The tracker's page: the epics on the person with their subtasks and progress, and the tasks on them — all of what the
// tracker holds, not a pick. Searched, filtered by status, tag, kind and epic, sorted, and grouped; a row is picked to
// be taken into a new chat.
import { useMemo } from 'react';
import {
  FilterMenu,
  PickMenu,
  PrefChoice,
  PrefSwitch,
  SearchField,
  SettingsMenu,
} from '@/next/features/apps/controls';
import { MoreList, Note } from '@/next/features/apps/list';
import {
  DEFAULT_TRACKER_QUERY,
  facet,
  filterTracker,
  groupTasks,
  rowsIn,
  stateText,
  tokens,
  TASK_GROUPS,
  TRACKER_SORTS,
  TRACKER_VIEWS,
  type ITrackerQuery,
} from '@/next/features/apps/model';
import PageHead, { type IPageHeadProps } from '@/next/features/apps/page-head';
import { setPrefs, STALE_CHOICES, type IAppsPrefs } from '@/next/features/apps/prefs';
import { EpicRow, TaskRow, type IRowProps } from '@/next/features/apps/rows';
import { t } from '@/next/i18n';
import type { AppFeed, AppRow } from '@/next/protocol/v2';
import Segmented from '@/next/ui/segmented';

export interface IPageProps<Q> {
  app: AppFeed;
  head: Omit<IPageHeadProps, 'children'>;
  now: number;
  staleDays: number;
  prefs: IAppsPrefs;
  /** What the person asked of this page; the screen keeps it while another app is open. */
  query: Q;
  onQuery: (query: Q) => void;
  picked: ReadonlySet<string>;
  onToggle: (key: string) => void;
  onChat: (rows: AppRow[]) => void;
}

const choice = <V extends string>(value: V, key: string) => ({ value, label: t(`${key}.${value}`) });

const TrackerPage = ({
  app,
  head,
  now,
  staleDays,
  prefs,
  query,
  onQuery,
  picked,
  onToggle,
  onChat,
}: IPageProps<ITrackerQuery>) => {
  const patch = (next: Partial<ITrackerQuery>) => onQuery({ ...query, ...next });
  const epics = rowsIn(app, 'epics');
  const tasks = rowsIn(app, 'tasks');
  const pool = query.view === 'epics' ? epics : tasks;
  const words = useMemo(() => tokens(query.q), [query.q]);
  const rows = useMemo(() => filterTracker(pool, query, prefs.showDone), [pool, query, prefs.showDone]);
  const facets = useMemo(
    () => ({
      states: facet(pool, (row) => [row.state]),
      labels: facet(pool, (row) => row.labels),
      kinds: facet(tasks, (row) => [row.kind]),
      epics: facet(tasks, (row) => [row.parent?.id ?? null]),
    }),
    [pool, tasks],
  );
  const epicTitles = useMemo(() => new Map(epics.map((epic) => [epic.id, epic.title])), [epics]);
  const sig = JSON.stringify([query, prefs.showDone]);
  const tasksView = query.view === 'tasks';
  const groups = useMemo(
    () => groupTasks(rows, tasksView ? query.group : 'none'),
    [rows, tasksView, query.group],
  );
  const sorts = TRACKER_SORTS.filter((sort) => !tasksView || sort !== 'progress');

  const rowProps = (row: AppRow, index: number): IRowProps => ({
    row,
    place: index,
    on: picked.has(row.key),
    now,
    staleDays,
    onToggle,
    onChat: (one) => onChat([one]),
  });

  const groupTitle = (key: string | null): string => {
    if (query.group === 'status') return key ? stateText(key) : t('next.apps.group.noStatus');
    return key ? `${key} · ${epicTitles.get(key) ?? ''}` : t('next.apps.group.noEpic');
  };

  const filtered = rows.length === 0 && pool.length > 0;
  return (
    <>
      <PageHead {...head}>
        <SettingsMenu label={t('next.apps.prefs.title')}>
          <PrefSwitch
            label={t('next.apps.prefs.showDone')}
            hint={t('next.apps.prefs.showDoneHint')}
            on={prefs.showDone}
            onChange={(showDone) => setPrefs({ showDone })}
          />
          <PrefSwitch
            label={t('next.apps.prefs.openEpics')}
            hint={t('next.apps.prefs.openEpicsHint')}
            on={prefs.openEpics}
            onChange={(openEpics) => setPrefs({ openEpics })}
          />
          <PrefChoice
            label={t('next.apps.prefs.stale')}
            hint={t('next.apps.prefs.staleHint')}
            value={String(staleDays)}
            options={STALE_CHOICES.map((days) => ({ value: String(days), label: String(days) }))}
            onChange={(days) =>
              setPrefs({ staleDays: STALE_CHOICES.find((one) => String(one) === days) ?? null })
            }
          />
        </SettingsMenu>
      </PageHead>
      <div className="hx-hubtools">
        <Segmented
          label={t('next.apps.view')}
          value={query.view}
          options={TRACKER_VIEWS.map((view) => ({ value: view, label: t(`next.apps.tracker.view.${view}`) }))}
          onChange={(view) => patch({ view, sort: 'updated', states: [], kinds: [], epics: [] })}
        />
        <SearchField
          value={query.q}
          onChange={(q) => patch({ q })}
          placeholder={t('next.apps.tracker.search')}
        />
      </div>
      <div className="hx-hubfilters">
        <FilterMenu
          label={t('next.apps.filter.status')}
          icon="circle-dot"
          choices={facets.states.map((state) => ({ value: state, label: stateText(state) }))}
          value={query.states}
          onChange={(states) => patch({ states })}
        />
        <FilterMenu
          label={t('next.apps.filter.tags')}
          icon="tag"
          choices={facets.labels.map((label) => ({ value: label, label }))}
          value={query.labels}
          onChange={(labels) => patch({ labels })}
        />
        {tasksView && (
          <>
            <FilterMenu
              label={t('next.apps.filter.kind')}
              icon="bug"
              choices={facets.kinds.map((kind) => ({ value: kind, label: kind }))}
              value={query.kinds}
              onChange={(kinds) => patch({ kinds })}
            />
            <FilterMenu
              label={t('next.apps.filter.epic')}
              icon="layers"
              choices={facets.epics.map((id) => ({ value: id, label: id, sub: epicTitles.get(id) ?? null }))}
              value={query.epics}
              onChange={(chosen) => patch({ epics: chosen })}
            />
          </>
        )}
        <span className="hx-spacer" />
        {tasksView && (
          <PickMenu
            label={t('next.apps.group.title')}
            icon="list-filter"
            choices={TASK_GROUPS.map((group) => choice(group, 'next.apps.group'))}
            value={query.group}
            onChange={(group) => patch({ group })}
          />
        )}
        <PickMenu
          label={t('next.apps.sort.menu')}
          icon="arrow-up-down"
          choices={sorts.map((sort) => choice(sort, 'next.apps.sort'))}
          value={query.sort}
          onChange={(sort) => patch({ sort })}
        />
      </div>
      {rows.length > 0 &&
        groups.map((group) => (
          <section className="hx-hubgroup" key={group.key ?? 'none'}>
            {tasksView && query.group !== 'none' && (
              <h3 className="hx-hubgroup__title">{groupTitle(group.key)}</h3>
            )}
            <div className="hx-hublist">
              <MoreList
                items={group.rows}
                sig={sig}
                render={(row, index) =>
                  tasksView ? (
                    <TaskRow key={row.key} {...rowProps(row, index)} />
                  ) : (
                    <EpicRow
                      key={row.key}
                      {...rowProps(row, index)}
                      words={words}
                      openByDefault={prefs.openEpics}
                    />
                  )
                }
              />
            </div>
          </section>
        ))}
      {filtered && (
        <Note
          icon="search"
          title={t('next.apps.nothing.title')}
          text={t('next.apps.nothing.filtered')}
          action={
            <button
              type="button"
              className="hx-hubnote__link"
              onClick={() => onQuery({ ...DEFAULT_TRACKER_QUERY, view: query.view })}
            >
              {t('next.apps.nothing.reset')}
            </button>
          }
        />
      )}
      {pool.length === 0 && (
        <Note
          icon={tasksView ? 'square-check-big' : 'layers'}
          title={t(`next.apps.empty.${query.view}.title`)}
          text={t(`next.apps.empty.${query.view}.text`)}
        />
      )}
    </>
  );
};

export default TrackerPage;
