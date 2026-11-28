// «Где работает чат»: here, or one of the person's computers through Harnessly Connect, with the folder on it. The
// chat keeps its history; its agent starts again there once its turn is over. A computer that is off cannot be picked.
import { useCallback, useEffect, useState } from 'react';
import Modal from '@/next/features/shell/modal';
import { Skeleton } from '@/next/ui/skeleton';
import type { IConnectChat } from '@/next/features/connect-chat/api';
import { t } from '@/next/i18n';
import '@/next/styles/handoff.css';
import '@/next/styles/chat-move.css';

export interface IConnectSheetProps {
  open: boolean;
  onClose: () => void;
  load: () => Promise<IConnectChat | null>;
  /** Switches the chat; rejects with the reason and leaves the sheet open. */
  onSwitch: (to: { device: string; dir: string } | null) => Promise<void>;
}

type TLoad = { k: 'loading' } | { k: 'failed'; reason: string } | { k: 'ready'; data: IConnectChat | null };

const HERE = '';

const SheetBody = ({ load, onSwitch, onClose }: Omit<IConnectSheetProps, 'open'>) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [picked, setPicked] = useState<string | null>(null);
  const [dirs, setDirs] = useState<Record<string, string>>({});
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    () =>
      load().then(
        (data) => setState({ k: 'ready', data }),
        (failure: unknown) =>
          setState({
            k: 'failed',
            reason:
              failure instanceof Error && failure.message ? failure.message : t('next.connectChat.failed'),
          }),
      ),
    [load],
  );
  useEffect(() => {
    void run();
  }, [run]);
  const retry = () => {
    setState({ k: 'loading' });
    void run();
  };

  const data = state.k === 'ready' ? state.data : null;
  const devices = data?.devices ?? [];
  const current = data?.on?.device ?? HERE;
  const choice = picked ?? current;
  const dirOf = (id: string): string => {
    if (dirs[id] !== undefined) return dirs[id];
    if (data?.on?.device === id) return data.on.dir;
    return devices.find((d) => d.id === id)?.suggest ?? '';
  };
  const dir = choice === HERE ? '' : dirOf(choice).trim();
  const changed = choice !== current || (choice !== HERE && dir !== (data?.on?.dir ?? ''));
  const canSend =
    data !== null &&
    data.claude &&
    !data.folder &&
    changed &&
    (choice === HERE || (dir.startsWith('/') && devices.some((d) => d.id === choice && d.online))) &&
    !sending;

  const send = async () => {
    if (!canSend) return;
    setError(null);
    setSending(true);
    try {
      await onSwitch(choice === HERE ? null : { device: choice, dir });
      onClose();
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.connectChat.failed'));
    } finally {
      setSending(false);
    }
  };

  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void send();
      }}
    >
      <div className="hx-modal__body hx-mv">
        {state.k === 'loading' && (
          <div className="hx-hsheet__wait" role="status">
            <Skeleton className="hx-hsheet__sk" />
            <span>{t('next.connectChat.loading')}</span>
          </div>
        )}
        {state.k === 'failed' && (
          <div className="hx-hsheet__fail" role="alert">
            <span>{state.reason}</span>
            <button type="button" className="hx-btn" data-variant="ghost" onClick={retry}>
              {t('next.load.retry')}
            </button>
          </div>
        )}
        {state.k === 'ready' && data === null && <p className="hx-mv__note">{t('next.connectChat.off')}</p>}
        {data && !data.claude && <p className="hx-mv__note">{t('next.connectChat.onlyClaude')}</p>}
        {data && data.folder && <p className="hx-mv__note">{t('next.connectChat.folder')}</p>}
        {data && data.claude && !data.folder && (
          <>
            <div className="hx-mv__list" role="radiogroup" aria-label={t('next.connectChat.title')}>
              <button
                type="button"
                role="radio"
                aria-checked={choice === HERE}
                className="hx-mv__vm"
                data-active={choice === HERE || undefined}
                disabled={sending}
                onClick={() => setPicked(HERE)}
              >
                <span className="hx-mv__name">{t('next.connectChat.here')}</span>
                <span className="hx-mv__why">{data.cwd}</span>
              </button>
              {devices.map((d) => (
                <button
                  key={d.id}
                  type="button"
                  role="radio"
                  aria-checked={choice === d.id}
                  className="hx-mv__vm"
                  data-active={choice === d.id || undefined}
                  disabled={sending || (!d.online && current !== d.id)}
                  onClick={() => setPicked(d.id)}
                >
                  <span className="hx-mv__name">{d.name}</span>
                  <span className="hx-mv__why">
                    {d.online
                      ? current === d.id
                        ? t('next.connectChat.now')
                        : d.os
                      : t('next.connectChat.offline')}
                  </span>
                </button>
              ))}
            </div>
            {devices.length === 0 && <p className="hx-mv__note">{t('next.connectChat.none')}</p>}
            {choice !== HERE && (
              <label className="hx-create__f">
                <span className="hx-create__l">{t('next.connectChat.dir')}</span>
                <input
                  className="hx-create__in hx-create__in--path"
                  type="text"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="/home/…"
                  value={dirOf(choice)}
                  disabled={sending}
                  onChange={(event) => setDirs({ ...dirs, [choice]: event.target.value })}
                />
              </label>
            )}
            {changed && data.busy && <p className="hx-mv__note">{t('next.connectChat.busy')}</p>}
            {changed && <p className="hx-mv__note">{t('next.connectChat.restart')}</p>}
          </>
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
          {t('next.rowMenu.cancel')}
        </button>
        <button
          type="submit"
          className="hx-btn hx-create__ok"
          data-variant="primary"
          data-busy={sending}
          disabled={!canSend}
        >
          <span>{t('next.connectChat.send')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      </div>
    </form>
  );
};

const ConnectSheet = ({ open, onClose, ...body }: IConnectSheetProps) => (
  <Modal
    open={open}
    onClose={onClose}
    title={t('next.connectChat.title')}
    lead={t('next.connectChat.lead')}
    boxClass="hx-create hx-mv-box"
  >
    <SheetBody {...body} onClose={onClose} />
  </Modal>
);

export default ConnectSheet;
