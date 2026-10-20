import { useEffect, useRef, useState } from 'react';
import { Check, Copy, ExternalLink, Globe, Link2Off, Lock, Share2 } from 'lucide-react';
import { fetchDocAccess, setDocPublic } from '@/lib/doc-threads-client';
import type { IDocAccess } from '@/types/doc-threads';
import { t } from '@/lib/i18n';

const COPIED_MS = 1800;

type TState =
  { status: 'loading' } | { status: 'ready'; access: IDocAccess } | { status: 'failed'; message: string };

const LOADING: TState = { status: 'loading' };

const messageOf = (err: unknown): string =>
  err instanceof Error && err.message ? err.message : t('docs.docShare.errorUnreachable');

/**
 * «Share». A document is closed by default and a person opens it behind the login; this button is the one place
 * where somebody decides to give it out without a login. The link is read-only and carries no comments, and closing it revokes
 * the old address for good, so the menu says both before it asks for anything.
 */
export const ShareMenu = ({ slug }: { slug: string }) => {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<TState>(LOADING);
  const [attempt, setAttempt] = useState(0);
  const [pending, setPending] = useState(false);
  const [copied, setCopied] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const requestKey = open ? `${attempt}:${slug}` : null;
  const [stateKey, setStateKey] = useState(requestKey);
  if (stateKey !== requestKey) {
    setStateKey(requestKey);
    if (requestKey !== null) setState(LOADING);
  }

  useEffect(() => {
    if (!open) return undefined;
    let current = true;
    fetchDocAccess(slug)
      .then((access) => {
        if (current) setState({ status: 'ready', access });
      })
      .catch((err: unknown) => {
        if (current) setState({ status: 'failed', message: messageOf(err) });
      });
    return () => {
      current = false;
    };
  }, [open, slug, attempt]);

  useEffect(() => {
    if (!open) return undefined;
    const onPress = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onPress);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onPress);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  const change = async (next: boolean) => {
    setPending(true);
    try {
      setState({ status: 'ready', access: await setDocPublic(slug, next) });
    } catch (err) {
      setState({ status: 'failed', message: messageOf(err) });
    } finally {
      setPending(false);
    }
  };

  const copy = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
      copiedTimer.current = setTimeout(() => setCopied(false), COPIED_MS);
    } catch {
      // Without clipboard access the link is still in the field, selected on focus.
    }
  };

  const access = state.status === 'ready' ? state.access : null;

  return (
    <div className="td-share" ref={rootRef}>
      <button
        type="button"
        className="td-icon"
        data-on={access?.public || undefined}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={t('docs.docShare.share')}
        onClick={() => setOpen((v) => !v)}
      >
        <Share2 />
      </button>
      {open && (
        <div className="td-share-pop" role="dialog" aria-label={t('docs.docShare.shareDocument')}>
          {state.status === 'loading' && <p className="td-share-note">{t('docs.docShare.checking')}</p>}
          {state.status === 'failed' && (
            <>
              <p className="td-version-error" role="alert">
                {state.message}
              </p>
              <button
                type="button"
                className="td-share-action"
                onClick={() => setAttempt((value) => value + 1)}
              >
                {t('docs.docShare.retry')}
              </button>
            </>
          )}
          {access && !access.public && (
            <>
              <p className="td-share-head">
                <Lock aria-hidden="true" /> {t('docs.docShare.closed')}
              </p>
              <p className="td-share-note">{t('docs.docShare.openNote')}</p>
              <button
                type="button"
                className="td-share-action"
                data-primary
                disabled={pending}
                onClick={() => void change(true)}
              >
                <Globe aria-hidden="true" /> {pending ? t('docs.docShare.opening') : t('docs.docShare.open')}
              </button>
            </>
          )}
          {access?.public && (
            <>
              <p className="td-share-head">
                <Globe aria-hidden="true" /> {t('docs.docShare.isOpen')}
              </p>
              {access.publicUrl ? (
                <div className="td-share-link">
                  <input
                    type="text"
                    readOnly
                    value={access.publicUrl}
                    aria-label={t('docs.docShare.publicLink')}
                    onFocus={(event) => event.currentTarget.select()}
                  />
                  <button
                    type="button"
                    className="td-icon"
                    title={copied ? t('docs.docShare.copied') : t('docs.docShare.copyLink')}
                    onClick={() => void copy(access.publicUrl as string)}
                  >
                    {copied ? <Check /> : <Copy />}
                  </button>
                  <a
                    className="td-icon"
                    href={access.publicUrl}
                    target="_blank"
                    rel="noreferrer"
                    title={t('docs.docShare.openAsGuest')}
                  >
                    <ExternalLink />
                  </a>
                </div>
              ) : (
                <p className="td-version-error" role="alert">
                  {t('docs.docShare.noLink')}
                </p>
              )}
              <p className="td-share-note">{t('docs.docShare.closeNote')}</p>
              <button
                type="button"
                className="td-share-action"
                disabled={pending}
                onClick={() => void change(false)}
              >
                <Link2Off aria-hidden="true" />{' '}
                {pending ? t('docs.docShare.closing') : t('docs.docShare.close')}
              </button>
            </>
          )}
        </div>
      )}
    </div>
  );
};
