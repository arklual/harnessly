// The team's catalog on /apps: what the team's members published for the team, with one action per row. The name is the
// largest thing; the action is a quiet button that waits for the machine. A machine that is in no team shows nothing.
import { Button } from '@/next/ui/button';
import type { ITeamState } from '@/next/features/app-catalog/use-team';
import { ROW_ACTION, rowsOf, type TRowAction } from '@/next/features/app-catalog/model';
import { t } from '@/next/i18n';
import '@/next/styles/app-catalog.css';

const ACTION_LABEL = {
  [ROW_ACTION.install]: 'next.appteam.install',
  [ROW_ACTION.uninstall]: 'next.appteam.uninstall',
  [ROW_ACTION.mute]: 'next.appteam.mute',
  [ROW_ACTION.unmute]: 'next.appteam.unmute',
} as const satisfies Record<TRowAction, string>;

const AppsCatalog = ({ team }: { team: ITeamState }) => {
  const { info, error, busy, failure, act } = team;
  if (error) return <p className="hx-appteam__none">{error}</p>;
  // Nothing to show before it loads, and for a machine in no team.
  if (info === null || info.space === '') return null;
  const rows = rowsOf(info);
  return (
    <section className="hx-appteam" aria-label={t('next.appteam.title')}>
      <h2 className="hx-appteam__h2">{t('next.appteam.title')}</h2>
      {rows.length === 0 && <p className="hx-appteam__none">{t('next.appteam.none')}</p>}
      <div className="hx-appteam__rows">
        {rows.map((row) => (
          <div
            key={`${row.entry.id}/${row.entry.channel}`}
            className="hx-appteam__row"
            data-muted={row.muted || undefined}
          >
            <span className="hx-appteam__text">
              <span className="hx-appteam__name">{row.entry.name}</span>
              {row.entry.description && <span className="hx-appteam__desc">{row.entry.description}</span>}
            </span>
            {row.action !== null && (
              <Button
                size="sm"
                variant={row.action === ROW_ACTION.install ? 'primary' : 'ghost'}
                busy={busy === row.entry.id}
                disabled={busy !== null && busy !== row.entry.id}
                onClick={() => void act(row)}
              >
                {t(ACTION_LABEL[row.action])}
              </Button>
            )}
          </div>
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
