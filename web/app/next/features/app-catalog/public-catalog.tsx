// The public catalog on /apps: what anyone offers, as cards, found with the header's search; nothing is drawn while it
// is empty.
// An install is two steps on purpose: the first opens the install screen with everything the app reaches and the
// warning that apps are not checked (HRN-849), the second installs.
import { useState } from 'react';
import { Button } from '@/next/ui/button';
import AppTile from '@/next/features/app-catalog/app-tile';
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

const PublicCatalog = ({
  catalog,
  installed,
  limit,
  onOpen,
}: {
  catalog: IPublicState;
  installed: readonly string[];
  /** Shows only this many (the empty page offers a few). */
  limit?: number;
  /** A card opens the page of the app. */
  onOpen?: (id: string) => void;
}) => {
  const { entries, error, busy, failure, install } = catalog;
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
  const all = (entries ?? []).filter((e) => !installed.includes(e.id));
  const rows = limit === undefined ? all : all.slice(0, limit);
  // The search is the header's; nothing is drawn for a catalog with nothing in it.
  return (
    <>
      {rows.length > 0 && (
        <section className="hx-appsec" aria-label={t('next.apppublic.title')}>
          <h2 className="hx-appsec__h2">{t('next.apppublic.title')}</h2>
          <div className="hx-appk-grid">
            {rows.map((entry) => (
              <AppTile
                key={entry.id}
                name={entry.name}
                line={entry.description}
                note={refusalOf(entry.check)}
                onOpen={onOpen && (() => onOpen(entry.id))}
                action={
                  refusalOf(entry.check) === null && (
                    <Button size="sm" disabled={busy !== null} onClick={() => setAsking(entry.id)}>
                      {t('next.apppublic.install')}
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
    </>
  );
};

export default PublicCatalog;
