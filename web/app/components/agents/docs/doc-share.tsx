import { lazy, Suspense, useEffect, useRef, useState } from 'react';
import { Check, Copy, ExternalLink, Globe, Link2Off, Lock, Share2 } from 'lucide-react';
import { prefixAppPath } from '@/lib/app-path';
import { fetchDocAccess, setDocPublic } from '@/lib/doc-threads-client';
import type { IDocAccess } from '@/types/doc-threads';
import { t } from '@/lib/i18n';
import { docRef, type TGuestRights } from '@/next/features/designly/access/api';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import { usePortalContainer } from '@/next/ui/portal';
import { toast } from '@/next/ui/toast';

const DocShareHost = lazy(() => import('@/components/agents/docs/doc-share-host'));

const COPIED_MS = 1800;

type TState =
  { status: 'loading' } | { status: 'ready'; access: IDocAccess } | { status: 'failed'; message: string };

const LOADING: TState = { status: 'loading' };

const messageOf = (err: unknown): string =>
  err instanceof Error && err.message ? err.message : t('docs.docShare.errorUnreachable');

/**
 * The old «Share» of a portal without the gateway's access model (a standalone runtime): one public link, read-only and
 * without comments, and closing it revokes the old address for good, so the menu says both before it asks for anything.
 */
const LegacyShareMenu = ({ slug }: { slug: string }) => {
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

/** What a guest link may allow: a document is read or commented by a guest, a board is shown read-only. */
const DOC_RIGHTS: readonly TGuestRights[] = ['view', 'comment'];
const BOARD_RIGHTS: readonly TGuestRights[] = ['view'];

type TLegacy =
  { status: 'loading' } | { status: 'ready'; access: IDocAccess } | { status: 'failed'; message: string };

/**
 * The catalog's old public link of a document, shown in the dialog only while it is open: it opens
 * the document without a login, besides the links with a term, and «Закрыть» revokes it for good. Nothing is shown while
 * it is closed: new links are made in «Настройки ссылок».
 */
const LegacyPublicLink = ({ slug }: { slug: string }) => {
  const [state, setState] = useState<TLegacy>({ status: 'loading' });
  const [pending, setPending] = useState(false);

  useEffect(() => {
    let current = true;
    fetchDocAccess(slug).then(
      (access) => current && setState({ status: 'ready', access }),
      (err: unknown) => current && setState({ status: 'failed', message: messageOf(err) }),
    );
    return () => {
      current = false;
    };
  }, [slug]);

  if (state.status !== 'ready' || !state.access.public) return null;
  const close = () => {
    setPending(true);
    setDocPublic(slug, false).then(
      (access) => {
        setPending(false);
        setState({ status: 'ready', access });
      },
      (err: unknown) => {
        setPending(false);
        toast(t('docs.docShare.closeFailed'), { tone: 'error', description: messageOf(err) });
      },
    );
  };
  return (
    <section className="hx-acc-sec" data-section="legacy">
      <h3 className="hx-acc-sec__title">{t('docs.docShare.legacyTitle')}</h3>
      <div className="hx-acc-row">
        <span className="hx-acc-av" data-tone="team" aria-hidden="true">
          <Globe />
        </span>
        <span className="hx-acc-row__who">
          <span>{t('docs.docShare.isOpen')}</span>
          <span className="hx-acc-row__sub">{t('docs.docShare.legacyNote')}</span>
        </span>
        <button
          type="button"
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          data-busy={pending || undefined}
          disabled={pending}
          onClick={close}
        >
          <span>{pending ? t('docs.docShare.closing') : t('docs.docShare.closeShort')}</span>
        </button>
      </div>
    </section>
  );
};

/** Where a document is read: the signed-in viewer, which asks for access when the person has none. */
const docAddress = (slug: string): string =>
  new URL(prefixAppPath(`/docs/${encodeURIComponent(slug)}`), window.location.href).href;

interface IDocShareProps {
  slug: string;
  title: string;
  kind: 'artifact' | 'board';
}

/**
 * «Поделиться» of a document or a board: the same dialog as a design's — people with their roles, what the team may
 * do («Только приглашённые» is «only me»), and links without sign-in with a term. Where the gateway
 * has no access model (a standalone runtime) a document keeps its old one-link menu and a board has no button.
 */
export const DocShare = ({ slug, title, kind }: IDocShareProps) => {
  const gate = useProjectAccess(docRef(slug));
  const { reload } = gate;
  const [open, setOpen] = useState(false);
  // Inside the new interface's frame the dialog has its place; on a page of its own it brings one.
  const framed = usePortalContainer() !== null;

  if (gate.state.k === ACCESS.absent) return kind === 'artifact' ? <LegacyShareMenu slug={slug} /> : null;
  const ready = gate.state.k === ACCESS.ready ? gate.state.access : null;

  const copyLink = () => {
    navigator.clipboard
      ?.writeText(docAddress(slug))
      .then(() => toast(t('next.site.linkCopied')))
      .catch((failure: Error) =>
        toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
      );
  };

  const dialog = ready && (
    <ShareDialog
      open={open}
      onClose={() => setOpen(false)}
      project={docRef(slug)}
      title={title}
      access={ready}
      onNext={gate.apply}
      onCopyLink={copyLink}
      onClaim={() => Promise.resolve()}
      guestRights={kind === 'artifact' ? DOC_RIGHTS : BOARD_RIGHTS}
      extra={kind === 'artifact' && open ? <LegacyPublicLink slug={slug} /> : null}
    />
  );

  return (
    <>
      <button
        type="button"
        className="td-icon"
        aria-haspopup="dialog"
        title={t('docs.docShare.share')}
        disabled={!ready}
        onClick={() => {
          // The dialog shows what the gateway holds now, not what it held when the document was opened.
          reload();
          setOpen(true);
        }}
      >
        <Share2 />
      </button>
      {dialog &&
        (framed ? (
          dialog
        ) : (
          <Suspense fallback={null}>
            <DocShareHost>{dialog}</DocShareHost>
          </Suspense>
        ))}
    </>
  );
};
