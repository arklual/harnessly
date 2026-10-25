// GitLab's page: the merge requests of the instance with what is known of each — draft, conflicts, pipeline, approvals,
// open discussions, the people on it, its labels. Narrowed to the ones that wait for the person, are theirs or on them;
// searched and filtered by state, author, assignee, project, label, pipeline and condition; sorted.
import { useMemo } from 'react';
import {
  FilterMenu,
  PickMenu,
  PrefChoice,
  PrefSwitch,
  SearchField,
  SettingsMenu,
} from '@/next/features/apps/controls';
import type { IPageProps } from '@/next/features/apps/tracker-page';
import { MoreList, Note } from '@/next/features/apps/list';
import {
  DEFAULT_MR_QUERY,
  facet,
  filterMrs,
  MR_CHECKS,
  MR_CONDITIONS,
  MR_SCOPES,
  MR_SORTS,
  MR_STATES,
  rowsIn,
  type IMrQuery,
} from '@/next/features/apps/model';
import PageHead from '@/next/features/apps/page-head';
import { setPrefs, STALE_CHOICES } from '@/next/features/apps/prefs';
import { MrRow } from '@/next/features/apps/rows';
import { t } from '@/next/i18n';
import type { AppRow } from '@/next/protocol/v2';
import Segmented from '@/next/ui/segmented';

const choice = <V extends string>(value: V, key: string) => ({ value, label: t(`${key}.${value}`) });

const GitlabPage = ({
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
}: IPageProps<IMrQuery>) => {
  const patch = (next: Partial<IMrQuery>) => onQuery({ ...query, ...next });
  const pool = rowsIn(app, 'mrs');
  const rows = useMemo(() => filterMrs(pool, query, prefs.hideDrafts), [pool, query, prefs.hideDrafts]);
  const facets = useMemo(
    () => ({
      authors: facet(pool, (row) => [row.author]),
      assignees: facet(pool, (row) => row.assignees),
      projects: facet(pool, (row) => [row.project]),
      labels: facet(pool, (row) => row.labels),
    }),
    [pool],
  );
  const sig = JSON.stringify([query, prefs.hideDrafts]);
  const names = (list: readonly string[]) => list.map((name) => ({ value: name, label: name }));
  const toChat = (row: AppRow) => onChat([row]);
  const reset = () => onQuery({ ...DEFAULT_MR_QUERY, scope: query.scope });

  return (
    <>
      <PageHead {...head}>
        <SettingsMenu label={t('next.apps.prefs.title')}>
          <PrefChoice
            label={t('next.apps.prefs.scope')}
            hint={t('next.apps.prefs.scopeHint')}
            value={prefs.scope}
            options={MR_SCOPES.map((scope) => choice(scope, 'next.apps.gitlab.scope'))}
            onChange={(scope) => setPrefs({ scope })}
          />
          <PrefSwitch
            label={t('next.apps.prefs.hideDrafts')}
            hint={t('next.apps.prefs.hideDraftsHint')}
            on={prefs.hideDrafts}
            onChange={(hideDrafts) => setPrefs({ hideDrafts })}
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
          value={query.scope}
          options={MR_SCOPES.map((scope) => choice(scope, 'next.apps.gitlab.scope'))}
          onChange={(scope) => patch({ scope })}
        />
        <SearchField
          value={query.q}
          onChange={(q) => patch({ q })}
          placeholder={t('next.apps.gitlab.search')}
        />
      </div>
      <div className="hx-hubfilters">
        <FilterMenu
          label={t('next.apps.filter.state')}
          icon="git-pull-request"
          choices={MR_STATES.map((state) => choice(state, 'next.apps.mr'))}
          value={query.states}
          onChange={(states) => patch({ states })}
        />
        <FilterMenu
          label={t('next.apps.filter.author')}
          icon="user"
          choices={names(facets.authors)}
          value={query.authors}
          onChange={(authors) => patch({ authors })}
        />
        <FilterMenu
          label={t('next.apps.filter.assignee')}
          icon="at-sign"
          choices={names(facets.assignees)}
          value={query.assignees}
          onChange={(assignees) => patch({ assignees })}
        />
        <FilterMenu
          label={t('next.apps.filter.project')}
          icon="folder-git-2"
          choices={names(facets.projects)}
          value={query.projects}
          onChange={(projects) => patch({ projects })}
        />
        <FilterMenu
          label={t('next.apps.filter.tags')}
          icon="tag"
          choices={names(facets.labels)}
          value={query.labels}
          onChange={(labels) => patch({ labels })}
        />
        <FilterMenu
          label={t('next.apps.filter.checks')}
          icon="circle-check"
          choices={MR_CHECKS.map((check) => choice(check, 'next.apps.checks'))}
          value={query.checks}
          onChange={(checks) => patch({ checks })}
        />
        <FilterMenu
          label={t('next.apps.filter.condition')}
          icon="list-checks"
          choices={MR_CONDITIONS.map((condition) => choice(condition, 'next.apps.condition'))}
          value={query.conditions}
          onChange={(conditions) => patch({ conditions })}
        />
        <span className="hx-spacer" />
        <PickMenu
          label={t('next.apps.sort.menu')}
          icon="arrow-up-down"
          choices={MR_SORTS.map((sort) => choice(sort, 'next.apps.sort'))}
          value={query.sort}
          onChange={(sort) => patch({ sort })}
        />
      </div>
      {rows.length > 0 && (
        <div className="hx-hublist">
          <MoreList
            items={rows}
            sig={sig}
            render={(row, index) => (
              <MrRow
                key={row.key}
                row={row}
                place={index}
                on={picked.has(row.key)}
                now={now}
                staleDays={staleDays}
                onToggle={onToggle}
                onChat={toChat}
              />
            )}
          />
        </div>
      )}
      {rows.length === 0 && pool.length > 0 && (
        <Note
          icon="search"
          title={t('next.apps.nothing.title')}
          text={t('next.apps.nothing.filtered')}
          action={
            <button type="button" className="hx-hubnote__link" onClick={reset}>
              {t('next.apps.nothing.reset')}
            </button>
          }
        />
      )}
      {pool.length === 0 && (
        <Note
          icon="git-pull-request"
          title={t('next.apps.empty.mrs.title')}
          text={t('next.apps.empty.mrs.text')}
        />
      )}
    </>
  );
};

export default GitlabPage;
