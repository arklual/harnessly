// «Telegram» (Settings → Notifications) with the shared bot: the person connects their Telegram with one
// button — the bot opens with a one-time code, Start links the chat — and writes one instruction of when agents should
// write there. No token anywhere on the page; the chat is shown by its title only. Every change waits for the answer.
import { useEffect, useId, useState } from 'react';
import type { ISharedState } from '@/next/features/notify-bots/api';
import { reasonText, Reveal, useAction, type TDone } from '@/next/features/settings/notify-bots-section';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import '@/next/styles/providers.css';
import '@/next/styles/notify-bots.css';

/** «Точно отключить?» waits this long for the second press. */
const SURE_MS = 4000;
/** «Отправлено» stays this long under the buttons. */
const SENT_MS = 4000;

export interface INotifySharedActions {
  /** A new one-time code: the link that opens the bot with it, or why there is none. */
  onLink: () => Promise<{ link: string } | { error: string }>;
  onUnlink: () => TDone;
  onSave: (instruction: string) => TDone;
  onTest: () => TDone;
}

export interface INotifySharedSectionProps extends INotifySharedActions {
  /** null while it is on its way. */
  state: ISharedState | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  /** Why the state is the one last seen ('' when it is fresh). */
  problem?: string;
}

/** A tab opened in the press itself (a tab opened after an await is blocked), sent to the link once it is known. */
const openTab = (): Window | null => {
  try {
    const tab = window.open('', '_blank');
    if (tab) tab.opener = null;
    return tab;
  } catch {
    return null;
  }
};

/** The chat: «Подключить Telegram», the wait for Start, or the connected chat with its test and «Отключить». */
const Connection = ({ state, actions }: { state: ISharedState; actions: INotifySharedActions }) => {
  const act = useAction();
  const [sure, setSure] = useState(false);
  const [sent, setSent] = useState(false);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const binding = state.chat === null && state.binding?.link !== dismissed ? state.binding : null;
  const [shownLink, setShownLink] = useState(binding?.link ?? '');
  if (binding !== null && shownLink !== binding.link) setShownLink(binding.link);
  const waiting = binding !== null;

  useEffect(() => {
    if (!sure) return;
    const timer = window.setTimeout(() => setSure(false), SURE_MS);
    return () => window.clearTimeout(timer);
  }, [sure]);
  useEffect(() => {
    if (!sent) return;
    const timer = window.setTimeout(() => setSent(false), SENT_MS);
    return () => window.clearTimeout(timer);
  }, [sent]);

  const connect = () => {
    const tab = openTab();
    setDismissed(null);
    void act.run('link', async () => {
      const got = await actions.onLink();
      if ('error' in got) {
        tab?.close();
        return got.error;
      }
      if (tab) tab.location.href = got.link;
      return null;
    });
  };
  const unlink = () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    void act.run('unlink', actions.onUnlink);
  };
  const test = async () => {
    setSent(false);
    if (await act.run('test', actions.onTest)) setSent(true);
  };

  return (
    <div className="hx-nb__bind">
      <Reveal open={state.chat === null && !waiting}>
        <div className="hx-nb__chat">
          <span className="hx-nb__chat-line" data-none>
            {t('next.notifyShared.none')}
          </span>
          <SetButton tone="primary" icon="send" busy={act.busy === 'link'} onClick={connect}>
            {t('next.notifyShared.connect')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={waiting}>
        <div className="hx-nb__wait">
          <div className="hx-nb__waiting" role="status">
            <Icon name="loader-circle" size="sm" className="hx-spin-ic" />
            <span>{t('next.notifyShared.waiting')}</span>
          </div>
          <p className="hx-nb__hint">
            {t('next.notifyShared.notOpened')}{' '}
            <a className="hx-nb__link" href={shownLink} target="_blank" rel="noreferrer">
              {t('next.notifyShared.openAgain')}
            </a>
          </p>
          <SetButton tone="ghost" onClick={() => setDismissed(shownLink)}>
            {t('next.notifyBots.cancel')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={state.chat !== null}>
        <div className="hx-nb__chat">
          <span className="hx-nb__chat-line">
            {t('next.notifyShared.linked', { chat: state.chat?.title ?? '' })}
          </span>
          <SetButton tone="ghost" icon="send" busy={act.busy === 'test'} onClick={() => void test()}>
            {t('next.notifyBots.test')}
          </SetButton>
          <SetButton tone="ghost" busy={act.busy === 'unlink'} onClick={unlink}>
            {sure ? t('next.notifyShared.unlinkSure') : t('next.notifyShared.unlink')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={sent}>
        <div className="hx-preset__test" data-ok role="status">
          <Icon name="circle-check" size="sm" />
          <span>{t('next.notifyBots.sent', { chat: state.chat?.title ?? '' })}</span>
        </div>
      </Reveal>
      <Reveal open={act.error !== null}>
        <FormError>{act.kept}</FormError>
      </Reveal>
    </div>
  );
};

/** The person's one instruction of when agents write. */
const Instruction = ({ value, onSave }: { value: string; onSave: INotifySharedActions['onSave'] }) => {
  const [draft, setDraft] = useState(value);
  const act = useAction();
  const id = useId();
  const changed = draft.trim() !== value;
  return (
    <div className="hx-nb__field">
      <label className="hx-nb__label" htmlFor={id}>
        {t('next.notifyBots.instruction')}
      </label>
      <textarea
        id={id}
        className="hx-set__input hx-nb__textarea"
        value={draft}
        rows={3}
        maxLength={2000}
        placeholder={t('next.notifyBots.instruction.placeholder')}
        onChange={(event) => {
          setDraft(event.target.value);
          act.clear();
        }}
      />
      <span className="hx-nb__hint">{t('next.notifyShared.instruction.hint')}</span>
      <Reveal open={changed}>
        <div className="hx-preset__row hx-nb__actions">
          <SetButton
            tone="primary"
            busy={act.busy === 'save'}
            onClick={() => void act.run('save', () => onSave(draft.trim()))}
          >
            {t('next.notifyBots.save')}
          </SetButton>
          <SetButton tone="ghost" onClick={() => setDraft(value)}>
            {t('next.notifyBots.cancel')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={act.error !== null}>
        <FormError>{act.kept}</FormError>
      </Reveal>
    </div>
  );
};

const NotifySharedSection = ({
  state,
  failed = null,
  problem = '',
  ...actions
}: INotifySharedSectionProps) => (
  <Group title={t('next.notifyBots.title')} foot={t('next.notifyShared.foot')}>
    {state === null && failed === null && <div className="hx-set__empty">{t('next.settings.loading')}</div>}
    {state === null && failed !== null && (
      <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
    )}
    {state !== null && (
      <div className="hx-nb-shared">
        <Connection state={state} actions={actions} />
        <Instruction key={state.instruction} value={state.instruction} onSave={actions.onSave} />
        {problem !== '' && <p className="hx-nb__hint">{reasonText(problem)}</p>}
      </div>
    )}
  </Group>
);

export default NotifySharedSection;
