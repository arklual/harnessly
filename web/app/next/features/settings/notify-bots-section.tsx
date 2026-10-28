// «Telegram» (Settings → Notifications): the bots the person linked so that agents can write to them, any number of
// them, each with the person's instruction of when to use it. A bot is linked by its token from @BotFather, then bound
// to a chat by a one-time code the person sends it with Start; the runtime finds the code in the bot's messages. Every
// change waits for the machine's answer; the token is typed here and never comes back.
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { NOTIFY_REASONS, type INotifyBot } from '@/next/features/notify-bots/api';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import { useLeavingList } from '@/next/ui/presence';
import SecretField from '@/next/ui/secret-field';
import '@/next/styles/providers.css';
import '@/next/styles/notify-bots.css';

/** A removed bot's row folds for as long as .hx-reveal's transition. */
const LEAVE_MS = 280;
/** «Точно удалить?» waits this long for the second press. */
const SURE_MS = 4000;
/** «Отправлено» stays this long under the buttons. */
const SENT_MS = 4000;
const BOTFATHER = 'https://t.me/BotFather';

/** What a refusal of the runtime says to the person. */
export const reasonText = (code: string): string =>
  NOTIFY_REASONS.some((reason) => reason === code)
    ? t(`next.notifyBots.reason.${code}`)
    : t('next.notifyBots.reason.other', { code });

/** Every action resolves null when the machine took it, else the words of why not. */
export type TDone = Promise<string | null>;

export interface INotifyBotsActions {
  onAdd: (token: string) => TDone;
  onSave: (id: string, change: { name: string; instruction: string }) => TDone;
  onToken: (id: string, token: string) => TDone;
  onBind: (id: string) => TDone;
  onStopBind: (id: string) => TDone;
  onTest: (id: string) => TDone;
  onRemove: (id: string) => TDone;
}

export interface INotifyBotsSectionProps extends INotifyBotsActions {
  /** null while the list is on its way. */
  bots: readonly INotifyBot[] | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  /** The bot whose card is open. */
  open: string | null;
  onOpen: (id: string | null) => void;
}

/** A local action's wait and its error line, kept on screen while the line folds away. */
export const useAction = () => {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [kept, setKept] = useState('');
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  if (error !== null && kept !== error) setKept(error);
  const run = async (key: string, action: () => TDone): Promise<boolean> => {
    setBusy(key);
    setError(null);
    const refused = await action();
    if (!alive.current) return false;
    setBusy(null);
    setError(refused);
    return refused === null;
  };
  return { busy, error, kept, run, clear: () => setError(null) };
};

export const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

const firstLine = (text: string) => text.split('\n').find((line) => line.trim() !== '') ?? '';

interface IBindProps {
  bot: INotifyBot;
  actions: INotifyBotsActions;
}

/** Where the bot writes, and the binding of a chat with its link and the wait for the person's Start. */
const Bind = ({ bot, actions }: IBindProps) => {
  const act = useAction();
  const waiting = bot.binding?.state === 'waiting';
  const failed = bot.binding?.state === 'failed' ? bot.binding.reason : null;
  const [binding, setBinding] = useState(bot.binding);
  if (bot.binding !== null && binding !== bot.binding) setBinding(bot.binding);
  let line = t('next.notifyBots.chat.none');
  if (bot.chat) line = t('next.notifyBots.chat.bound', { chat: bot.chat.title });
  return (
    <div className="hx-nb__bind">
      <div className="hx-nb__label">{t('next.notifyBots.chat.label')}</div>
      <Reveal open={!waiting}>
        <div className="hx-nb__chat">
          <span className="hx-nb__chat-line" data-none={bot.chat === null || undefined}>
            {line}
          </span>
          <SetButton
            tone={bot.chat ? 'ghost' : 'primary'}
            icon="link"
            busy={act.busy === 'bind'}
            onClick={() => void act.run('bind', () => actions.onBind(bot.id))}
          >
            {bot.chat ? t('next.notifyBots.chat.rebind') : t('next.notifyBots.chat.bind')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={waiting}>
        <div className="hx-nb__wait">
          <a
            className="hx-set__btn hx-nb__open"
            data-tone="primary"
            href={binding?.link}
            target="_blank"
            rel="noreferrer"
          >
            <Icon name="send" size="sm" />
            <span className="hx-set__btn-label">{t('next.notifyBots.bind.open')}</span>
          </a>
          <p className="hx-nb__hint">
            {t('next.notifyBots.bind.group')} <code className="hx-nb__code">{binding?.command}</code>
          </p>
          <div className="hx-nb__waiting" role="status">
            <Icon name="loader-circle" size="sm" className="hx-spin-ic" />
            <span>{t('next.notifyBots.bind.waiting')}</span>
            <SetButton
              tone="ghost"
              busy={act.busy === 'stop'}
              onClick={() => void act.run('stop', () => actions.onStopBind(bot.id))}
            >
              {t('next.notifyBots.cancel')}
            </SetButton>
          </div>
        </div>
      </Reveal>
      <Reveal open={failed !== null && !waiting}>
        <FormError>{reasonText(failed ?? binding?.reason ?? '')}</FormError>
      </Reveal>
      <Reveal open={act.error !== null}>
        <FormError>{act.kept}</FormError>
      </Reveal>
    </div>
  );
};

/** The token as its last characters, and a write-only field to replace it. */
const Token = ({ bot, actions }: IBindProps) => {
  const [replacing, setReplacing] = useState(false);
  const [draft, setDraft] = useState('');
  const act = useAction();
  const save = async () => {
    const token = draft.trim();
    if (token === '') return;
    // The token leaves the page's state the moment it is sent.
    setDraft('');
    if (await act.run('token', () => actions.onToken(bot.id, token))) setReplacing(false);
  };
  return (
    <div className="hx-nb__token">
      <div className="hx-nb__label">{t('next.notifyBots.token.label')}</div>
      <Reveal open={!replacing}>
        <div className="hx-nb__chat">
          <span className="hx-nb__chat-line">
            {t('next.notifyBots.token.saved', { username: bot.username, tail: bot.tokenTail })}
          </span>
          <SetButton tone="ghost" onClick={() => setReplacing(true)}>
            {t('next.notifyBots.token.replace')}
          </SetButton>
        </div>
      </Reveal>
      <Reveal open={replacing}>
        <div className="hx-nb__form">
          <SecretField
            value={draft}
            onChange={(value) => {
              setDraft(value);
              act.clear();
            }}
            label={t('next.notifyBots.token.field')}
            placeholder={t('next.notifyBots.token.placeholder')}
            disabled={act.busy !== null}
            invalid={act.error !== null}
            onSubmit={() => void save()}
          />
          <div className="hx-preset__row">
            <SetButton
              tone="primary"
              busy={act.busy === 'token'}
              disabled={draft.trim() === ''}
              onClick={() => void save()}
            >
              {t('next.notifyBots.token.save')}
            </SetButton>
            <SetButton
              tone="ghost"
              onClick={() => {
                setReplacing(false);
                setDraft('');
                act.clear();
              }}
            >
              {t('next.notifyBots.cancel')}
            </SetButton>
          </div>
        </div>
      </Reveal>
      <Reveal open={act.error !== null}>
        <FormError>{act.kept}</FormError>
      </Reveal>
    </div>
  );
};

/** An open bot: its name, its instruction, its chat, its token, and the test and removal. */
const Editor = ({ bot, actions }: IBindProps) => {
  const [name, setName] = useState(bot.name);
  const [instruction, setInstruction] = useState(bot.instruction);
  const [sure, setSure] = useState(false);
  const [sent, setSent] = useState(false);
  const act = useAction();
  const ids = useId();
  const changed = name.trim() !== bot.name || instruction.trim() !== bot.instruction;

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

  const remove = () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    void act.run('remove', () => actions.onRemove(bot.id));
  };
  const test = async () => {
    setSent(false);
    if (await act.run('test', () => actions.onTest(bot.id))) setSent(true);
  };

  return (
    <div className="hx-nb__body">
      <div className="hx-nb__field">
        <label className="hx-nb__label" htmlFor={`${ids}-name`}>
          {t('next.notifyBots.name')}
        </label>
        <input
          id={`${ids}-name`}
          className="hx-set__input"
          value={name}
          maxLength={64}
          onChange={(event) => setName(event.target.value)}
        />
      </div>
      <div className="hx-nb__field">
        <label className="hx-nb__label" htmlFor={`${ids}-instruction`}>
          {t('next.notifyBots.instruction')}
        </label>
        <textarea
          id={`${ids}-instruction`}
          className="hx-set__input hx-nb__textarea"
          value={instruction}
          rows={3}
          maxLength={2000}
          placeholder={t('next.notifyBots.instruction.placeholder')}
          onChange={(event) => setInstruction(event.target.value)}
        />
        <span className="hx-nb__hint">{t('next.notifyBots.instruction.hint')}</span>
      </div>
      <Bind bot={bot} actions={actions} />
      <Token bot={bot} actions={actions} />
      <div>
        <div className="hx-preset__row hx-nb__actions">
          <SetButton
            tone="primary"
            busy={act.busy === 'save'}
            disabled={!changed || name.trim() === ''}
            onClick={() =>
              void act.run('save', () =>
                actions.onSave(bot.id, { name: name.trim(), instruction: instruction.trim() }),
              )
            }
          >
            {t('next.notifyBots.save')}
          </SetButton>
          {bot.chat && (
            <SetButton tone="ghost" icon="send" busy={act.busy === 'test'} onClick={() => void test()}>
              {t('next.notifyBots.test')}
            </SetButton>
          )}
          <SetButton tone="ghost" icon="trash-2" busy={act.busy === 'remove'} onClick={remove}>
            {sure ? t('next.notifyBots.removeSure') : t('next.notifyBots.remove')}
          </SetButton>
        </div>
        <Reveal open={sent}>
          <div className="hx-preset__test" data-ok role="status">
            <Icon name="circle-check" size="sm" />
            <span>{t('next.notifyBots.sent', { chat: bot.chat?.title ?? '' })}</span>
          </div>
        </Reveal>
        <Reveal open={act.error !== null}>
          <FormError>{act.kept}</FormError>
        </Reveal>
      </div>
    </div>
  );
};

interface IRowProps {
  bot: INotifyBot;
  open: boolean;
  leaving: boolean;
  actions: INotifyBotsActions;
  onOpen: (id: string | null) => void;
}

const BotRow = ({ bot, open, leaving, actions, onOpen }: IRowProps) => {
  let sub = t('next.notifyBots.sub.noChat', { username: bot.username });
  if (bot.chat) sub = t('next.notifyBots.sub.chat', { username: bot.username, chat: bot.chat.title });
  const instruction = firstLine(bot.instruction);
  return (
    <div className="hx-reveal hx-nb-wrap" data-open={!leaving}>
      <div className="hx-reveal__in">
        <div className="hx-nb" data-open={open}>
          <button
            type="button"
            className="hx-nb__head"
            aria-expanded={open}
            disabled={leaving}
            onClick={() => onOpen(open ? null : bot.id)}
          >
            <span className="hx-nb__ic">
              <Icon name="send" size="sm" />
            </span>
            <span className="hx-nb__text">
              <span className="hx-nb__name">{bot.name}</span>
              <span className="hx-nb__sub">{sub}</span>
              {instruction !== '' && <span className="hx-nb__instr">{instruction}</span>}
            </span>
            <Icon name="chevron-down" size="sm" className="hx-nb__chev" />
          </button>
          <Reveal open={open && !leaving}>
            <Editor key={`${bot.id}:${bot.name}:${bot.instruction}`} bot={bot} actions={actions} />
          </Reveal>
        </div>
      </div>
    </div>
  );
};

/** «Добавить бота»: the hint to @BotFather and the token's field. */
const AddBot = ({ onAdd }: { onAdd: INotifyBotsActions['onAdd'] }) => {
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState('');
  const act = useAction();
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (adding) field.current?.focus();
  }, [adding]);
  const add = async () => {
    const token = draft.trim();
    if (token === '') return;
    setDraft('');
    if (await act.run('add', () => onAdd(token))) setAdding(false);
  };
  const cancel = () => {
    setAdding(false);
    setDraft('');
    act.clear();
  };
  return (
    <div className="hx-nb-add">
      <Reveal open={!adding}>
        <button type="button" className="hx-nb-add__btn" onClick={() => setAdding(true)}>
          <Icon name="plus" size="sm" />
          {t('next.notifyBots.add')}
        </button>
      </Reveal>
      <Reveal open={adding}>
        <div className="hx-nb__form hx-nb-add__form">
          <p className="hx-nb__hint">
            {t('next.notifyBots.add.hintBefore')}{' '}
            <a className="hx-nb__link" href={BOTFATHER} target="_blank" rel="noreferrer">
              @BotFather
            </a>{' '}
            {t('next.notifyBots.add.hintAfter')}
          </p>
          <SecretField
            ref={field}
            value={draft}
            onChange={(value) => {
              setDraft(value);
              act.clear();
            }}
            label={t('next.notifyBots.token.field')}
            placeholder={t('next.notifyBots.token.placeholder')}
            disabled={act.busy !== null}
            invalid={act.error !== null}
            onSubmit={() => void add()}
          />
          <div className="hx-preset__row">
            <SetButton
              tone="primary"
              busy={act.busy === 'add'}
              disabled={draft.trim() === ''}
              onClick={() => void add()}
            >
              {t('next.notifyBots.connect')}
            </SetButton>
            <SetButton tone="ghost" onClick={cancel}>
              {t('next.notifyBots.cancel')}
            </SetButton>
          </div>
          <Reveal open={act.error !== null}>
            <FormError>{act.kept}</FormError>
          </Reveal>
        </div>
      </Reveal>
    </div>
  );
};

const NotifyBotsSection = ({ bots, failed = null, open, onOpen, ...actions }: INotifyBotsSectionProps) => {
  const rows = useLeavingList(bots ?? [], (bot) => bot.id, LEAVE_MS);
  return (
    <Group title={t('next.notifyBots.title')} foot={t('next.notifyBots.foot')}>
      {bots === null && failed === null && <div className="hx-set__empty">{t('next.settings.loading')}</div>}
      {bots === null && failed !== null && (
        <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
      )}
      {bots !== null && (
        <>
          {bots.length === 0 && <p className="hx-nb-empty">{t('next.notifyBots.empty')}</p>}
          {rows.map(({ item, leaving }) => (
            <BotRow
              key={item.id}
              bot={item}
              open={open === item.id}
              leaving={leaving}
              actions={actions}
              onOpen={onOpen}
            />
          ))}
          <AddBot onAdd={actions.onAdd} />
        </>
      )}
    </Group>
  );
};

export default NotifyBotsSection;
