// «Поделиться» for a file of the chat's folder: the file becomes a document of the catalog, private to its
// owner, and the same dialog as designs and documents opens for it. A save of the file is a new version of the document
// until «Отвязать». Nothing is drawn while the document is being made; a refusal is a toast in the runtime's words
// (a secret names its line).
import { useEffect, useState } from 'react';
import { ACCESS, useProjectAccess } from '@/next/features/designly/access/use-access';
import { docRef } from '@/next/features/designly/access/api';
import ShareDialog from '@/next/features/designly/access/share-dialog';
import { shareFileAsDoc, unlinkFileDoc, type IFileDoc } from '@/next/features/filedoc/api';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export interface IFileShareProps {
  machine: MachineId;
  conv: ConvId;
  /** The file's path in the chat's folder. */
  path: string;
  open: boolean;
  onClose: () => void;
}

const baseName = (path: string): string => path.slice(path.lastIndexOf('/') + 1);

/** Where a document is read: the signed-in page of the gateway, which asks for access when the person has none. */
const docAddress = (slug: string): string =>
  new URL(`/agents/docs/${encodeURIComponent(slug)}`, window.location.href).href;

interface IDialogProps extends IFileShareProps {
  doc: IFileDoc & { slug: string };
  onDoc: (doc: IFileDoc) => void;
}

const Dialog = ({ machine, conv, path, open, onClose, doc, onDoc }: IDialogProps) => {
  const gate = useProjectAccess(docRef(doc.slug));
  const { reload } = gate;
  // The dialog shows what the gateway holds now, not what it held when it was last open.
  useEffect(() => {
    if (open) reload();
  }, [open, reload]);
  const [unlinking, setUnlinking] = useState(false);
  if (gate.state.k !== ACCESS.ready) return null;

  const copyLink = () => {
    navigator.clipboard
      ?.writeText(docAddress(doc.slug))
      .then(() => toast(t('next.site.linkCopied')))
      .catch((failure: Error) =>
        toast(t('next.site.copyFailed'), { tone: 'error', description: failure.message }),
      );
  };
  const unlink = () => {
    setUnlinking(true);
    unlinkFileDoc(machine, conv, path).then(
      (next) => {
        setUnlinking(false);
        onDoc(next);
        toast(t('next.share.unlinked'));
      },
      (failure: Error) => {
        setUnlinking(false);
        toast(t('next.share.failed'), { tone: 'error', description: failure.message });
      },
    );
  };
  return (
    <ShareDialog
      open={open}
      onClose={onClose}
      project={docRef(doc.slug)}
      title={baseName(path)}
      access={gate.state.access}
      onNext={gate.apply}
      onCopyLink={copyLink}
      onClaim={() => Promise.resolve()}
      footExtra={
        doc.linked && (
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-busy={unlinking || undefined}
            disabled={unlinking}
            onClick={unlink}
          >
            <span>{t('next.share.unlink')}</span>
          </button>
        )
      }
    />
  );
};

const FileShare = (props: IFileShareProps) => {
  const { machine, conv, path, open, onClose } = props;
  const [doc, setDoc] = useState<{ path: string; doc: IFileDoc } | null>(null);

  // Opening makes the document (or finds the one the file already has).
  useEffect(() => {
    if (!open) return;
    const abort = new AbortController();
    shareFileAsDoc(machine, conv, path, abort.signal).then(
      (made) => setDoc({ path, doc: made }),
      (failure: Error) => {
        if (abort.signal.aborted) return;
        toast(t('next.share.failed'), { tone: 'error', description: failure.message });
        onClose();
      },
    );
    return () => abort.abort();
    // onClose is the host's; a new one each render must not make the document again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, machine, conv, path]);

  const shown = doc?.path === path ? doc.doc : null;
  if (!shown?.slug) return null;
  return (
    <Dialog {...props} doc={{ ...shown, slug: shown.slug }} onDoc={(next) => setDoc({ path, doc: next })} />
  );
};

export default FileShare;

/** The pane header's «Поделиться» of an open file, with its dialog. */
export const FileShareButton = ({ machine, conv, path }: Omit<IFileShareProps, 'open' | 'onClose'>) => {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="ghost" size="sm" onClick={() => setOpen(true)}>
        {t('next.share.button')}
      </Button>
      <FileShare machine={machine} conv={conv} path={path} open={open} onClose={() => setOpen(false)} />
    </>
  );
};
