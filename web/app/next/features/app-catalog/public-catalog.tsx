// The public catalog on /apps: what anyone offers, with a search and one action per row. The name is the largest thing.
// An install is two steps on purpose: the first opens the install screen with everything the app reaches and the
// warning that apps are not checked, the second installs.
import { useState } from 'react';
import { Button } from '@/next/ui/button';
import type { IPublicEntry } from '@/next/features/app-catalog/api';
import type { IPublicState } from '@/next/features/app-catalog/use-public';
import { refusalOf } from '@/next/features/app-catalog/model';
import ConsentSheet from '@/next/features/app-consent/consent-sheet';
import type { IAccess } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';
import '@/next/styles/app-catalog.css';

/** What an entry reaches in words; an older machine gives only the rights' names, shown as they are. */
const accessOf = (entry: IPublicEntry): IAccess[] =>
  entry.check?.access ??
  entry.permissions.map((p) => ({ key: `perm:${p}`, code: p, class: 'medium', label: { ru: p, en: p } }));

const PublicCatalog = ({ catalog, installed }: { catalog: IPublicState; installed: readonly string[] }) => {
  const { entries, query, setQuery, error, busy, failure, install } = catalog;
  // The row that shows its rights, waiting for the person's word.
  const [asking, setAsking] = useState<string | null>(null);
  const shown = (entries ?? []).find((e) => e.id === asking) ?? null;
  const [closing, setClosing] = useState<IPublicEntry | null>(null);
  const onScreen = shown ?? closing;
  const close = () => {
    setClosing(shown);
    setAsking(null);
  };
  const confirm = async () => {
    if (!shown) return;
    await install(shown);
    close();
  };
  if (error) return <p className="hx-appteam__none">{error}</p>;
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
            </div>
          );
        })}
      </div>
      {failure && (
        <p className="hx-appteam__fail" role="alert">
          {failure}
        </p>
      )}
      <ConsentSheet
        open={shown !== null}
        app={
          onScreen && { name: onScreen.name, version: onScreen.version, source: t('next.apppublic.title') }
        }
        access={onScreen ? accessOf(onScreen) : []}
        onConfirm={confirm}
        onCancel={close}
      />
    </section>
  );
};

export default PublicCatalog;
