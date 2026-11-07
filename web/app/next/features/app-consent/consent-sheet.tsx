// The install screen, the one place where an app gets what it reaches: who and from where, the warning that
// apps are not checked, everything the app reaches in words from the most dangerous down, «Установить» and «Отмена».
// An update shows the same with only what it adds, marked «новое»; without the person's word the old version keeps
// running. No «accept all without looking» and no unticking of single lines: an app is made for its set.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import Sheet from '@/next/ui/sheet';
import Tooltip from '@/next/ui/tooltip';
import { Button } from '@/next/ui/button';
import AccessList, { threatsOf } from '@/next/features/app-consent/access-list';
import type { IAccess } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';

export interface IConsentApp {
  name: string;
  icon?: string;
  version: string;
  /** Where it comes from: a folder, a catalog, an address. */
  source?: string;
  dev?: boolean;
}

export interface IConsentSheetProps {
  open: boolean;
  app: IConsentApp | null;
  /** All the app reaches (an install), or only what an update adds. */
  access: readonly IAccess[];
  update?: boolean;
  /** Rejects with the text to show. */
  onConfirm: () => Promise<void>;
  /** «Отмена»: the person says no. */
  onCancel: () => void;
  /** The scrim, Esc or a swipe: the sheet closes and nothing is decided (onCancel when absent). */
  onClose?: () => void;
}

/** The hosts whose servers carry a view of the app: their code changes without a new version. */
export const remoteHostsOf = (access: readonly IAccess[]): string[] =>
  access.filter((a) => a.key.startsWith('frame:')).map((a) => a.key.slice('frame:'.length));

/** A long folder is cut in the middle: its root (a home, /tmp) and its own name stay seen, the whole path is on hover. */
export const sourceOf = (source: string): string => {
  if (!source.startsWith('/')) return source;
  const parts = source.split('/').filter((p) => p !== '');
  return parts.length > 4 ? `/${parts.slice(0, 2).join('/')}/…/${parts.slice(-2).join('/')}` : source;
};

const ConsentSheet = ({
  open,
  app,
  access,
  update = false,
  onConfirm,
  onCancel,
  onClose,
}: IConsentSheetProps) => {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const threats = threatsOf(access);
  const remote = remoteHostsOf(access);
  const confirm = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await onConfirm();
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const meta = [
    app ? t('next.consent.version', { version: app.version }) : '',
    app?.source ? t('next.consent.from', { source: sourceOf(app.source) }) : '',
    app?.dev ? t('next.consent.draft') : '',
  ].filter((s) => s !== '');
  const line = <span>{meta.join(' · ')}</span>;
  return (
    <Sheet open={open} onClose={onClose ?? onCancel} label={app?.name ?? ''} className="hx-consent">
      {app && (
        <div className="hx-consent__in">
          <div className="hx-consent__head">
            <span className="hx-appcard__tile">
              <Icon name={app.icon || 'layout-grid'} />
            </span>
            <span className="hx-consent__title">
              <strong>{update ? t('next.consent.updateTitle', { name: app.name }) : app.name}</strong>
              {app.source && sourceOf(app.source) !== app.source ? (
                <Tooltip label={app.source}>{line}</Tooltip>
              ) : (
                line
              )}
            </span>
          </div>
          <div className="hx-consent__warn" role="note">
            <Icon name="triangle-alert" size="sm" />
            <p>
              {t('next.consent.warning')}
              {threats.length > 0 && ` ${threats.join(' ')}`}
            </p>
          </div>
          {remote.length > 0 && (
            <p className="hx-consent__remote">{t('next.consent.remote', { hosts: remote.join(', ') })}</p>
          )}
          {update && <p className="hx-consent__note">{t('next.consent.updateNote')}</p>}
          <AccessList access={access} fresh={update} />
          {failure && (
            <p className="hx-consent__fail" role="alert">
              {failure}
            </p>
          )}
          <div className="hx-consent__actions">
            <Button variant="ghost" disabled={busy} onClick={onCancel}>
              {t('next.consent.cancel')}
            </Button>
            <Button variant="primary" busy={busy} onClick={() => void confirm()}>
              {update ? t('next.consent.update') : t('next.consent.install')}
            </Button>
          </div>
        </div>
      )}
    </Sheet>
  );
};

export default ConsentSheet;
