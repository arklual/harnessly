// «Поделиться» of a board or a document from the app's header, a list of the app or a card in the chat: the
// same dialog as the viewer's and Sinopia's — people and teams with their roles, links without sign-in — not a link to
// the app on this machine. «Копировать ссылку» copies the board's or the document's own page on its service
// (<boards>/b/<slug>, <documents>/a/<slug>): it opens for whoever has access and asks the others to sign in. Where there is no
// access model (a standalone runtime) the link the caller gives is copied, as before.
import { useCallback } from 'react';
import { DocShareDialog, type TDocShareKind } from '@/next/features/app-main/doc-share-dialog';
import { useLinkCardMeta } from '@/next/features/chat/link-card-meta';
import { appBasePath, appConfig } from '@/platform/config';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export type TShareKind = TDocShareKind;

/** The page of a board or a document on its service, without a board's key (access is the hub's to give); the hub's
 *  reader where the service's address is not known. */
export const objectAddress = (kind: TShareKind, slug: string): string => {
  const config = appConfig();
  const base = kind === 'board' ? config.boardsUrl : config.artifactsUrl;
  if (base) return `${base.replace(/\/+$/, '')}/${kind === 'board' ? 'b' : 'a'}/${encodeURIComponent(slug)}`;
  return `${window.location.origin}${appBasePath()}/docs/${encodeURIComponent(slug)}`;
};

export interface IObjectShareProps {
  kind: TShareKind;
  slug: string;
  open: boolean;
  onClose: () => void;
  /** Where there is no access model: the link to copy instead of the dialog. */
  fallbackLink: string;
}

const ObjectShare = ({ kind, slug, open, onClose, fallbackLink }: IObjectShareProps) => {
  const meta = useLinkCardMeta(slug);
  const onReady = useCallback(
    (ready: boolean) => {
      if (ready || !open) return;
      // No access model here: the old behaviour, the link to the app.
      onClose();
      navigator.clipboard.writeText(fallbackLink).then(
        () => toast(t('next.appmain.shared'), { description: t('next.appmain.sharedHint') }),
        () => toast(t('next.appmain.shareFailed'), { tone: 'error', description: fallbackLink }),
      );
    },
    [open, onClose, fallbackLink],
  );
  return (
    <DocShareDialog
      slug={slug}
      title={meta?.title ?? slug}
      kind={meta?.kind ?? kind}
      open={open}
      onClose={onClose}
      onReady={onReady}
      address={objectAddress(meta?.kind ?? kind, slug)}
    />
  );
};

export default ObjectShare;
