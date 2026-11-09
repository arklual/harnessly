// «Поделиться» of a chat (beta «chatShare»): a snapshot of the transcript with its secrets hidden, a Markdown or HTML
// file of it, and a read-only link for 7, 30 or 90 days. The dialog changes nothing in the chat itself; every step waits
// for the runtime and says what it answered.
import { useState } from 'react';
import {
  DEFAULT_OPTIONS,
  EXPIRY_DAYS,
  captureSnapshot,
  downloadUrl,
  previewUrl,
  publishShare,
  type IDraft,
  type IShare,
  type ISnapshotOptions,
  type TExpiryDays,
} from '@/next/features/chat-share/api';
import Modal from '@/next/features/shell/modal';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

export interface IChatShare {
  machine: MachineId;
  tabId: string;
}

const FLAGS = ['thinking', 'diffs', 'economics'] as const;

const ShareForm = ({ share, onClose }: { share: IChatShare; onClose: () => void }) => {
  const [options, setOptions] = useState<ISnapshotOptions>(DEFAULT_OPTIONS);
  const [draft, setDraft] = useState<IDraft | null>(null);
  const [days, setDays] = useState<TExpiryDays>(7);
  const [link, setLink] = useState<IShare | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const say = (code: string) =>
    setError(
      code === 'unavailable' ? t('next.chatShare.unavailable') : t('next.chatShare.failed', { why: code }),
    );

  const capture = async () => {
    setBusy(true);
    setError(null);
    setLink(null);
    const res = await captureSnapshot(share.machine, share.tabId, options);
    setBusy(false);
    if ('ok' in res) setDraft(res.ok);
    else say(res.error);
  };

  const publish = async () => {
    if (!draft) return;
    setBusy(true);
    setError(null);
    const res = await publishShare(share.machine, draft.id, days);
    setBusy(false);
    if ('ok' in res) setLink(res.ok);
    else say(res.error);
  };

  const copy = (url: string) => {
    void navigator.clipboard?.writeText(url).then(() => toast(t('next.chatShare.copied')));
  };

  return (
    <div>
      <div className="hx-modal__body">
        <fieldset disabled={busy} className="hx-create__f">
          <label className="hx-create__l">
            <input
              type="checkbox"
              checked={options.tools === 'summary'}
              onChange={(e) => setOptions({ ...options, tools: e.target.checked ? 'summary' : 'none' })}
            />{' '}
            {t('next.chatShare.opt.tools')}
          </label>
          {FLAGS.map((flag) => (
            <label key={flag} className="hx-create__l">
              <input
                type="checkbox"
                checked={options[flag]}
                onChange={(e) => setOptions({ ...options, [flag]: e.target.checked })}
              />{' '}
              {t(`next.chatShare.opt.${flag}`)}
            </label>
          ))}
        </fieldset>
        {draft && (
          <p className="hx-goal__note" data-testid="share-report">
            {t('next.chatShare.report', { hidden: draft.hidden, shortened: draft.shortened })}
          </p>
        )}
        {draft && (
          <p>
            <a href={previewUrl(share.machine, draft.id)} target="_blank" rel="noopener noreferrer">
              {t('next.chatShare.preview')}
            </a>
            {' · '}
            <a href={downloadUrl(share.machine, draft.id, 'md')}>Markdown</a>
            {' · '}
            <a href={downloadUrl(share.machine, draft.id, 'html')}>HTML</a>
          </p>
        )}
        {draft && !link && (
          <label className="hx-create__f">
            <span className="hx-create__l">{t('next.chatShare.days')}</span>
            <select
              value={days}
              disabled={busy}
              onChange={(e) => setDays(Number(e.target.value) as TExpiryDays)}
            >
              {EXPIRY_DAYS.map((d) => (
                <option key={d} value={d}>
                  {t('next.chatShare.daysN', { n: d })}
                </option>
              ))}
            </select>
          </label>
        )}
        {link && (
          <p>
            <input
              readOnly
              value={link.url}
              aria-label={t('next.chatShare.link')}
              onFocus={(e) => e.target.select()}
            />{' '}
            <button type="button" className="hx-btn" onClick={() => copy(link.url)}>
              {t('next.chatShare.copy')}
            </button>
          </p>
        )}
        {error && (
          <p className="hx-create__err" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot">
        <span className="hx-spacer" />
        <button type="button" className="hx-btn" data-variant="ghost" onClick={onClose}>
          {t('next.chatShare.close')}
        </button>
        <button
          type="button"
          className="hx-btn"
          data-busy={busy}
          disabled={busy}
          onClick={() => void capture()}
        >
          {draft ? t('next.chatShare.recapture') : t('next.chatShare.capture')}
        </button>
        {draft && !link && (
          <button
            type="button"
            className="hx-btn"
            data-variant="primary"
            disabled={busy}
            onClick={() => void publish()}
          >
            {t('next.chatShare.publish')}
          </button>
        )}
      </div>
    </div>
  );
};

const ChatShareDialog = ({
  open,
  share,
  onClose,
}: {
  open: boolean;
  share: IChatShare;
  onClose: () => void;
}) => (
  <Modal
    open={open}
    onClose={onClose}
    title={t('next.chatShare.title')}
    lead={t('next.chatShare.lead')}
    boxClass="hx-create"
  >
    <ShareForm share={share} onClose={onClose} />
  </Modal>
);

export default ChatShareDialog;
