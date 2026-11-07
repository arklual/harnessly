// The public catalog on /apps: what anyone offers, with a search and one action per row. The name is the largest thing.
// An install is two steps on purpose: the first shows the rights the app asks for, the second installs.
import { useState } from 'react';
import { Button } from '@/next/ui/button';
import type { IPublicEntry } from '@/next/features/app-catalog/api';
import type { IPublicState } from '@/next/features/app-catalog/use-public';
import { refusalOf, secretsOf } from '@/next/features/app-catalog/model';
import { t } from '@/next/i18n';
import '@/next/styles/app-catalog.css';

const PublicCatalog = ({ catalog, installed }: { catalog: IPublicState; installed: readonly string[] }) => {
  const { entries, query, setQuery, error, busy, failure, install } = catalog;
  // The row that shows its rights, waiting for the person's word.
  const [asking, setAsking] = useState<string | null>(null);
  if (error) return <p className="hx-appteam__none">{error}</p>;
  const confirm = async (entry: IPublicEntry) => {
    await install(entry);
    setAsking(null);
  };
  return (
    <section className="hx-appteam" aria-label={t('next.apppublic.title')}>
      <h2 className="hx-appteam__h2">{t('next.apppublic.title')}</h2>
      <input
        className="hx-appteam__search"
        type="search"
        value={query}
        placeholder={t('next.apppublic.search')}
        aria-label={t('next.apppublic.search')}
        onChange={(e) => setQuery(e.target.value)}
      />
      {entries !== null && entries.length === 0 && (
        <p className="hx-appteam__none">{t('next.apppublic.none')}</p>
      )}
      <div className="hx-appteam__rows">
        {(entries ?? []).map((entry) => {
          const have = installed.includes(entry.id);
          return (
            <div
              key={entry.id}
              className="hx-appteam__row hx-appteam__row--stack"
              data-open={asking === entry.id || undefined}
            >
              <span className="hx-appteam__line">
                <span className="hx-appteam__text">
                  <span className="hx-appteam__name">{entry.name}</span>
                  {entry.description && <span className="hx-appteam__desc">{entry.description}</span>}
                  {!have && refusalOf(entry.check) !== null && (
                    <span className="hx-appteam__desc">{refusalOf(entry.check)}</span>
                  )}
                </span>
                {!have && asking !== entry.id && refusalOf(entry.check) === null && (
                  <Button
                    size="sm"
                    variant="primary"
                    disabled={busy !== null}
                    onClick={() => setAsking(entry.id)}
                  >
                    {t('next.apppublic.install')}
                  </Button>
                )}
              </span>
              {asking === entry.id && (
                <span className="hx-appteam__ask">
                  <span className="hx-appteam__rights">
                    {entry.permissions.length === 0
                      ? t('next.apppublic.norights')
                      : t('next.apppublic.rights', { rights: entry.permissions.join(', ') })}
                    {secretsOf(entry.check) !== null && ` ${secretsOf(entry.check)}`}
                  </span>
                  <Button size="sm" variant="ghost" disabled={busy !== null} onClick={() => setAsking(null)}>
                    {t('next.apppublic.cancel')}
                  </Button>
                  <Button
                    size="sm"
                    variant="primary"
                    busy={busy === entry.id}
                    onClick={() => void confirm(entry)}
                  >
                    {t('next.apppublic.confirm')}
                  </Button>
                </span>
              )}
            </div>
          );
        })}
      </div>
      {failure && (
        <p className="hx-appteam__fail" role="alert">
          {failure}
        </p>
      )}
    </section>
  );
};

export default PublicCatalog;
