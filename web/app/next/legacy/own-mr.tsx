// The merge request of a chat's own branch at the foot of the «Ревью» panel: with one, «Снять черновик» and «Одобрить»
// (the code host's own state, through the person's connection); without one, «Создать MR» from the pushed branch.
import { useCallback, useState } from 'react';
import { forgeOfRemote } from '@/next/features/review/app/mr-review';
import { MrFooter, refusalText } from '@/next/features/review/app/mr-threads';
import { useMrContext, useMrReview, type IMrReviewIo } from '@/next/features/review/app/use-mr-review';
import { actMrReview, loadMrReview, openMergeRequest } from '@/next/legacy/code-review-api';
import { goTo, ROUTE } from '@/next/routes/routes';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

const IO: IMrReviewIo = { load: loadMrReview, act: actMrReview };
const FORM_EXIT_MS = 160;

/** The code host a change lives on, to sign in to it; null for what is no address. */
const hostRoot = (url: string): string | null => {
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
};

/** Connections; with a change, opened on its code host found, so the person signs in to it with their own token. */
export const toConnections = (change?: string) => {
  const connect = change ? hostRoot(change) : null;
  goTo({ kind: ROUTE.settings, section: 'connections', ...(connect ? { connect } : {}) });
};

export const OwnMr = ({ url }: { url: string }) => {
  const mr = useMrReview(url, IO);
  const connect = useCallback(() => toConnections(url), [url]);
  const ctx = useMrContext(mr, connect);
  if (!ctx) return null;
  return <MrFooter ctx={ctx} showPublish={false} />;
};

interface ICreateMrProps {
  remote: string;
  branch: string;
  /** What the title starts as: the branch's last commit. */
  title: string;
  onCreated: (url: string) => void;
}

export const CreateMr = ({ remote, branch, title: first, onCreated }: ICreateMrProps) => {
  const [open, setOpen] = useState(false);
  const form = usePresentFlag(open, FORM_EXIT_MS);
  const [title, setTitle] = useState(first);
  const [target, setTarget] = useState('main');
  const [draft, setDraft] = useState(true);
  const [busy, setBusy] = useState(false);
  const create = async () => {
    if (!title.trim() || !target.trim() || busy) return;
    setBusy(true);
    const res = await openMergeRequest({
      remote,
      source: branch,
      target: target.trim(),
      title: title.trim(),
      draft,
    });
    setBusy(false);
    if (res.ok) {
      setOpen(false);
      toast(t('next.reviewApp.mr.created'), {
        action: {
          label: t('next.reviewApp.mr.openIt'),
          onClick: () => goTo({ kind: ROUTE.review, url: res.url }),
        },
      });
      onCreated(res.url);
      return;
    }
    const text = refusalText(res.refusal, forgeOfRemote(remote));
    if (res.refusal.k === 'no-personal')
      toast(text, {
        tone: 'error',
        action: { label: t('next.reviewApp.connect'), onClick: () => toConnections() },
      });
    else toast(text, { tone: 'error' });
  };
  if (!form.mounted)
    return (
      <Button icon="git-pull-request" onClick={() => setOpen(true)}>
        {t('next.reviewApp.mr.create')}
      </Button>
    );
  return (
    <form
      className="hx-rvmr__create"
      data-state={form.state}
      onSubmit={(event) => {
        event.preventDefault();
        void create();
      }}
    >
      <label className="hx-rvmr__field">
        <span>{t('next.reviewApp.mr.title')}</span>
        <input
          className="hx-rvnote__field"
          value={title}
          onChange={(event) => setTitle(event.target.value)}
        />
      </label>
      <button type="submit" hidden aria-hidden="true" tabIndex={-1} />
      <label className="hx-rvmr__field">
        <span>{t('next.reviewApp.mr.into', { branch })}</span>
        <input
          className="hx-rvnote__field"
          value={target}
          onChange={(event) => setTarget(event.target.value)}
        />
      </label>
      <button
        type="button"
        className="hx-rvapp__barbtn"
        aria-pressed={draft}
        onClick={() => setDraft(!draft)}
      >
        <Icon name={draft ? 'square-check-big' : 'square'} size="sm" />
        <span>{t('next.reviewApp.mr.asDraft')}</span>
      </button>
      <div className="hx-rvnote__actions">
        <Button size="sm" variant="ghost" onClick={() => setOpen(false)}>
          {t('next.reviewApp.notes.cancel')}
        </Button>
        <Button
          size="sm"
          variant="primary"
          busy={busy}
          disabled={!title.trim() || !target.trim()}
          onClick={() => void create()}
        >
          {t('next.reviewApp.mr.create')}
        </Button>
      </div>
    </form>
  );
};
