// The team's catalog on /apps: what the team's members published for the team, as the same cards as the installed apps,
// with one quiet action each. A machine that is in no team, and a catalog with nothing in it, show nothing.
import { Button } from '@/next/ui/button';
import AppTile from '@/next/features/app-catalog/app-tile';
import type { ITeamState } from '@/next/features/app-catalog/use-team';
import { refusalOf, ROW_ACTION, rowsOf, secretsOf, type TRowAction } from '@/next/features/app-catalog/model';
import { t } from '@/next/i18n';
import '@/next/styles/app-catalog.css';

const ACTION_LABEL = {
  [ROW_ACTION.install]: 'next.appteam.install',
  [ROW_ACTION.uninstall]: 'next.appteam.uninstall',
  [ROW_ACTION.mute]: 'next.appteam.mute',
  [ROW_ACTION.unmute]: 'next.appteam.unmute',
} as const satisfies Record<TRowAction, string>;

/** What the search keeps: the name or the line has the words. */
export const matches = (query: string, ...texts: readonly string[]): boolean => {
  const q = query.trim().toLowerCase();
  return q === '' || texts.some((x) => x.toLowerCase().includes(q));
};

interface IAppsCatalogProps {
  team: ITeamState;
  /** The header's search. */
  query?: string;
  /** Shows only this many (the empty page offers a few). */
  limit?: number;
  /** A card opens the page of the app. */
  onOpen?: (id: string) => void;
}

const AppsCatalog = ({ team, query = '', limit, onOpen }: IAppsCatalogProps) => {
  const { info, error, busy, failure, act } = team;
  if (error) return <p className="hx-appteam__none">{error}</p>;
  // Nothing to show before it loads, and for a machine in no team.
  if (info === null || info.space === '') return null;
  // What the team already has is among the person's apps, with its switches in the details.
  const all = rowsOf(info).filter(
    (row) => !row.entry.installed && matches(query, row.entry.name, row.entry.description),
  );
  const rows = limit === undefined ? all : all.slice(0, limit);
  if (rows.length === 0) return null;
  return (
    <section className="hx-appsec" aria-label={t('next.appteam.title')}>
      <h2 className="hx-appsec__h2">{t('next.appteam.title')}</h2>
      <div className="hx-appk-grid">
        {rows.map((row) => (
          <AppTile
            key={`${row.entry.id}/${row.entry.channel}`}
            name={row.entry.name}
            line={row.entry.description}
            note={!row.entry.installed ? (refusalOf(row.entry.check) ?? secretsOf(row.entry.check)) : null}
            muted={row.muted}
            onOpen={onOpen && (() => onOpen(row.entry.id))}
            action={
              row.action !== null && (
                <Button
                  size="sm"
                  variant={row.action === ROW_ACTION.install ? 'default' : 'ghost'}
                  busy={busy === row.entry.id}
                  disabled={busy !== null && busy !== row.entry.id}
                  onClick={() => void act(row)}
                >
                  {t(ACTION_LABEL[row.action])}
                </Button>
              )
            }
          />
        ))}
      </div>
      {failure && (
        <p className="hx-appteam__fail" role="alert">
          {failure}
        </p>
      )}
    </section>
  );
};

export default AppsCatalog;
