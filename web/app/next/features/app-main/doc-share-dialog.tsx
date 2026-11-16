// «Поделиться» of a document or a board: the one dialog of every kind — people and teams with their
// roles, the home team first, links without sign-in per recipient (a document's own «Скачивание» and «Копирование
// текста»). A gateway without the common access API keeps the older dialog. It is the same wherever a board or a
// document is shared from: the viewer's button, the app's header, a card in the chat, a list of the app.
import { useEffect, type ReactNode } from 'react';
import { docRef, type TGuestRights } from '@/next/features/designly/access/api';
import { LINK_KIND, type ILinkContext } from '@/next/features/designly/access/link-form';
import ResourceShare from '@/next/features/designly/access/resource-share';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import { isResourceView, useResourceView } from '@/next/features/designly/access/use-resource';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

/** What a guest link may allow, a document's and a board's alike: read, or read and comment. */
const GUEST_RIGHTS: readonly TGuestRights[] = ['view', 'comment'];
const DOC_LINKS: ILinkContext = { kind: LINK_KIND.doc };
const BOARD_LINKS: ILinkContext = { kind: LINK_KIND.board };

export type TDocShareKind = 'artifact' | 'board';

export interface IDocShareDialogProps {
  slug: string;
  title: string;
  kind: TDocShareKind;
  open: boolean;
  onClose: () => void;
  /** The address «Копировать ссылку» copies: the board's or the document's own page (<service>/b/<slug> or /a/<slug>). */
  address: string;
  /** Called once the access is read: false where there is no access model (a standalone runtime). */
  onReady?: (ready: boolean) => void;
  /** More of the dialog under its sections (the viewer's old public link of a document). */
  extra?: ReactNode;
}

export const DocShareDialog = ({
  slug,
  title,
  kind,
  open,
  onClose,
  address,
  onReady,
  extra,
}: IDocShareDialogProps) => {
  const gate = useProjectAccess(docRef(slug));
  const { reload } = gate;
  const shared = useResourceView('doc', slug, gate.state.k === ACCESS.ready);
  const { reload: reloadShared } = shared;
  const one = isResourceView(shared.state) && shared.state.owner !== null ? shared.state : null;
  // The dialog shows what the gateway holds now, not what it held when it was last open.
  useEffect(() => {
    if (!open) return;
    reload();
    reloadShared();
  }, [open, reload, reloadShared]);
  const settled = gate.state.k === ACCESS.ready || gate.state.k === ACCESS.absent;
  const isReady = gate.state.k === ACCESS.ready;
  useEffect(() => {
    if (settled) onReady?.(isReady);
  }, [settled, isReady, onReady]);

  const ready = gate.state.k === ACCESS.ready ? gate.state.access : null;
  const copyLink = () => {
    navigator.clipboard
      ?.writeText(address)
      .then(() => toast(t('next.site.linkCopied')))
      .catch((failure: Error) =>
        toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
      );
  };

  if (one)
    return (
      <ResourceShare
        open={open}
        onClose={onClose}
        target={{ kind: 'doc', id: slug }}
        title={title}
        view={one}
        onNext={(next) => {
          shared.apply(next);
          reload();
        }}
        address={address}
        onCopyLink={copyLink}
        linkContext={kind === 'board' ? BOARD_LINKS : DOC_LINKS}
        extra={extra}
      />
    );
  if (!ready) return null;
  return (
    <ShareDialog
      open={open}
      onClose={onClose}
      project={docRef(slug)}
      title={title}
      access={ready}
      onNext={gate.apply}
      onCopyLink={copyLink}
      onClaim={() => Promise.resolve()}
      guestRights={GUEST_RIGHTS}
      extra={extra}
    />
  );
};
