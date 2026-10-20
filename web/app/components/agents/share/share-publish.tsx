import { useCallback, useEffect, useState } from 'react';
import { Check, Copy, Link2 } from 'lucide-react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHOICE_ACTIVATION,
  TwButton,
  TwSegmented,
  type ITwChoice,
} from '@/components/agents/ui';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import type { IShareDraftView } from '@/types/server/hl-share-drafts';
import type { IShareView, TShareExpiryDays } from '@/types/server/hl-share-store';
import { locale, plural, t } from '@/lib/i18n';

type TExpiryValue = '7' | '30' | '90';

const EXPIRY_CHOICES: readonly ITwChoice<TExpiryValue>[] = [
  { value: '7', label: plural(7, 'share.sharePublish.days') },
  { value: '30', label: plural(30, 'share.sharePublish.days') },
  { value: '90', label: plural(90, 'share.sharePublish.days') },
] as const;

const VIEWERS_SHOWN = 3;

type TLinksState =
  | { status: 'loading' }
  | { status: 'error'; message: string }
  | { status: 'ready'; enabled: boolean; shares: IShareView[] };

/** One pessimistic action at a time: which button spins and where its error goes. */
type TAction =
  { status: 'idle' } | { status: 'pending'; key: string } | { status: 'error'; key: string; message: string };

const PUBLISH_KEY = 'publish';

const dateFormat = new Intl.DateTimeFormat(locale() === 'ru' ? 'ru-RU' : 'en-GB', {
  day: '2-digit',
  month: '2-digit',
});

const errorMessage = async (response: Response) => {
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  return typeof body?.error === 'string'
    ? body.error
    : t('share.sharePublish.failedStatus', { status: response.status });
};

const shortUser = (sub: string) => sub.replace(/@.*$/, '');

const viewsLine = (share: IShareView) => {
  if (share.views === 0) return t('share.sharePublish.notOpened');
  const names = share.viewers.slice(0, VIEWERS_SHOWN).map((viewer) => shortUser(viewer.sub));
  const more =
    share.viewers.length > VIEWERS_SHOWN
      ? ` ${t('share.sharePublish.andMore', { count: share.viewers.length - VIEWERS_SHOWN })}`
      : '';
  return `${plural(share.views, 'share.sharePublish.views')} · ${names.join(', ')}${more}`;
};

const displayUrl = (url: string) => url.replace(/^https:\/\//, '');

interface IShareLinkRowProps {
  share: IShareView;
  draft: IShareDraftView | null;
  action: TAction;
  onReplace: (share: IShareView) => void;
  onRevoke: (share: IShareView) => void;
}

const ShareLinkRow = ({ share, draft, action, onReplace, onRevoke }: IShareLinkRowProps) => {
  const [copy, setCopy] = useState<'idle' | 'done' | 'error'>('idle');
  const [confirming, setConfirming] = useState(false);
  const url = share.url ?? '';
  const replaceKey = `replace:${share.id}`;
  const revokeKey = `revoke:${share.id}`;
  const rowError =
    action.status === 'error' && (action.key === replaceKey || action.key === revokeKey)
      ? action.message
      : null;

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopy('done');
    } catch {
      setCopy('error');
    }
  };

  return (
    <li className="tw-share-link">
      <div className="tw-share-link-url">
        <Link2 aria-hidden="true" />
        <a href={url} target="_blank" rel="noopener noreferrer" title={url}>
          {displayUrl(url)}
        </a>
        <TwButton
          size={BUTTON_SIZE.sm}
          icon={copy === 'done' ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
          onClick={copyUrl}
          aria-label={t('share.sharePublish.copyLink')}
        >
          {copy === 'done' ? t('share.sharePublish.copied') : t('share.sharePublish.copy')}
        </TwButton>
      </div>
      <p className="tw-share-link-meta">
        {viewsLine(share)} · {t('share.sharePublish.until', { date: dateFormat.format(share.expiresAt) })}
        {share.version > 1 && ` · ${t('share.sharePublish.version', { version: share.version })}`}
      </p>
      <div className="tw-share-link-actions">
        {draft && (
          <TwButton
            size={BUTTON_SIZE.sm}
            pending={action.status === 'pending' && action.key === replaceKey}
            onClick={() => onReplace(share)}
          >
            {t('share.sharePublish.replace')}
          </TwButton>
        )}
        {confirming ? (
          <>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.danger}
              pending={action.status === 'pending' && action.key === revokeKey}
              onClick={() => onRevoke(share)}
            >
              {t('share.sharePublish.confirmRevoke')}
            </TwButton>
            <TwButton
              size={BUTTON_SIZE.sm}
              variant={BUTTON_VARIANT.ghost}
              onClick={() => setConfirming(false)}
            >
              {t('share.sharePublish.cancel')}
            </TwButton>
          </>
        ) : (
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={() => setConfirming(true)}>
            {t('share.sharePublish.revoke')}
          </TwButton>
        )}
      </div>
      {copy === 'error' && (
        <p className="tw-share-pop-error" role="alert">
          {t('share.sharePublish.clipboardUnavailable')}
        </p>
      )}
      {rowError && (
        <p className="tw-share-pop-error" role="alert">
          {rowError}
        </p>
      )}
    </li>
  );
};

interface ISharePublishProps {
  tabId: string;
  machineId: string | undefined;
  /** A ready draft can be published as a new link or put behind an existing one. */
  draft: IShareDraftView | null;
  /** The draft was used up by a publish: the popover must build a new one for the next. */
  onDraftUsed: () => void;
}

/** «Link for colleagues»: publish a draft as a frozen read-only link, copy, views, replace, revoke. */
const SharePublish = ({ tabId, machineId, draft, onDraftUsed }: ISharePublishProps) => {
  const [links, setLinks] = useState<TLinksState>({ status: 'loading' });
  const [expiry, setExpiry] = useState<TExpiryValue>('30');
  const [action, setAction] = useState<TAction>({ status: 'idle' });

  const sharesPath = (tail = '') => scopedAppPath(`/api/platform/shares${tail}`, machineId);

  const fetchLinks = useCallback(async (): Promise<TLinksState> => {
    try {
      const response = await appFetch(
        scopedAppPath(`/api/platform/shares?tabId=${encodeURIComponent(tabId)}`, machineId),
      );
      if (!response.ok) return { status: 'error', message: await errorMessage(response) };
      const body = (await response.json()) as { enabled: boolean; shares: IShareView[] };
      return { status: 'ready', enabled: body.enabled, shares: body.shares };
    } catch {
      return { status: 'error', message: t('share.sharePublish.machineNoAnswer') };
    }
  }, [machineId, tabId]);

  const load = async () => setLinks(await fetchLinks());

  useEffect(() => {
    let alive = true;
    void fetchLinks().then((state) => {
      if (alive) setLinks(state);
    });
    return () => {
      alive = false;
    };
  }, [fetchLinks]);

  const run = async (key: string, request: () => Promise<Response>, after: () => void) => {
    setAction({ status: 'pending', key });
    try {
      const response = await request();
      if (!response.ok) {
        setAction({ status: 'error', key, message: await errorMessage(response) });
        return;
      }
      after();
      setAction({ status: 'idle' });
      await load();
    } catch {
      setAction({ status: 'error', key, message: t('share.sharePublish.machineNoAnswerRetry') });
    }
  };

  const publish = (replaceId: string | null) => {
    if (!draft) return;
    const expiresInDays = Number(expiry) as TShareExpiryDays;
    void run(
      replaceId ? `replace:${replaceId}` : PUBLISH_KEY,
      () =>
        appFetch(sharesPath(), {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ draftId: draft.id, expiresInDays, replaceId }),
        }),
      onDraftUsed,
    );
  };

  const revoke = (share: IShareView) =>
    void run(
      `revoke:${share.id}`,
      () => appFetch(sharesPath(`/${encodeURIComponent(share.id)}`), { method: 'DELETE' }),
      () => undefined,
    );

  if (links.status === 'loading') {
    return (
      <section className="tw-share-publish" aria-busy="true" aria-label={t('share.sharePublish.links')}>
        <div className="tw-share-publish-skeleton tw-skeleton" />
      </section>
    );
  }
  if (links.status === 'error') {
    return (
      <section className="tw-share-publish" aria-label={t('share.sharePublish.links')}>
        <p className="tw-share-pop-error" role="alert">
          {t('share.sharePublish.linksFailed', { message: links.message })}
        </p>
        <TwButton size={BUTTON_SIZE.sm} onClick={() => void load()}>
          {t('share.sharePublish.retry')}
        </TwButton>
      </section>
    );
  }
  if (!links.enabled) {
    return (
      <section className="tw-share-publish" aria-label={t('share.sharePublish.links')}>
        <p className="tw-share-pop-caption">{t('share.sharePublish.linksDisabled')}</p>
      </section>
    );
  }

  const live = links.shares.filter((share) => share.url !== null);
  const publishError = action.status === 'error' && action.key === PUBLISH_KEY ? action.message : null;

  return (
    <section className="tw-share-publish" aria-label={t('share.sharePublish.links')}>
      <h3 className="tw-share-publish-title">{t('share.sharePublish.title')}</h3>
      {draft && (
        <>
          <div className="tw-share-pop-field">
            <span className="tw-share-pop-label">{t('share.sharePublish.expiry')}</span>
            <TwSegmented
              options={EXPIRY_CHOICES}
              value={expiry}
              onChange={setExpiry}
              aria-label={t('share.sharePublish.expiryLabel')}
              activation={CHOICE_ACTIVATION.auto}
            />
          </div>
          <TwButton
            variant={BUTTON_VARIANT.primary}
            pending={action.status === 'pending' && action.key === PUBLISH_KEY}
            onClick={() => publish(null)}
            className="tw-share-pop-primary"
          >
            {live.length > 0 ? t('share.sharePublish.publishNew') : t('share.sharePublish.publish')}
          </TwButton>
          {publishError && (
            <p className="tw-share-pop-error" role="alert">
              {publishError}
            </p>
          )}
        </>
      )}
      {live.length > 0 && (
        <ul className="tw-share-links">
          {live.map((share) => (
            <ShareLinkRow
              key={share.id}
              share={share}
              draft={draft}
              action={action}
              onReplace={(target) => publish(target.id)}
              onRevoke={revoke}
            />
          ))}
        </ul>
      )}
      {!draft && live.length === 0 && (
        <p className="tw-share-pop-caption">{t('share.sharePublish.prepareFirst')}</p>
      )}
      <p className="tw-share-pop-caption">{t('share.sharePublish.accessNote')}</p>
    </section>
  );
};

export default SharePublish;
