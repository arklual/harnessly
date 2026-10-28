// «Сейф» (Settings → Safe): the secrets the person gave the agents of this machine — a name, when it was put, who used
// it last — with a new value (rotation) and removal. A value is typed here and never comes back: the machine answers
// with the name and the dates only. Agents ask for a missing secret with a card (ask-dialog), not through the chat.
import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { SAFE_REASONS, SECRET_NAME, type ISecret } from '@/next/features/safe/api';
import { FormError, Group, SetButton } from '@/next/features/settings/rows';
import { formatAgoFull } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import { useLeavingList } from '@/next/ui/presence';
import SecretField from '@/next/ui/secret-field';
import '@/next/styles/providers.css';
import '@/next/styles/safe.css';

/** A removed secret's row folds for as long as .hx-reveal's transition. */
const LEAVE_MS = 280;
/** «Точно удалить?» waits this long for the second press. */
const SURE_MS = 4000;

/** What a refusal of the runtime says to the person. */
export const safeReasonText = (code: string): string =>
  SAFE_REASONS.some((reason) => reason === code)
    ? t(`next.safe.reason.${code}`)
    : t('next.safe.reason.other', { code });

/** Every action resolves null when the machine took it, else the words of why not. */
type TDone = Promise<string | null>;

export interface ISafeActions {
  onPut: (name: string, value: string) => TDone;
  onRemove: (name: string) => TDone;
}

export interface ISafeSectionProps extends ISafeActions {
  /** null while the list is on its way. */
  secrets: readonly ISecret[] | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  now: number;
}

/** A local action's wait and its error line, kept on screen while the line folds away. */
const useAction = () => {
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

const Reveal = ({ open, children }: { open: boolean; children: ReactNode }) => (
  <div className="hx-reveal" data-open={open}>
    <div className="hx-reveal__in" inert={!open || undefined}>
      {children}
    </div>
  </div>
);

/** «Добавлен вчера», or «Заменён 2 ч назад» after a new value. */
export const putLine = (secret: ISecret, now: number): string => {
  const ago = formatAgoFull(secret.updatedAt, now);
  return secret.updatedAt > secret.createdAt ? t('next.safe.replaced', { ago }) : t('next.safe.put', { ago });
};

/** «Использовал «Чат» · 5 мин назад», or that nobody has yet. */
export const usedLine = (secret: ISecret, now: number): string => {
  if (secret.lastUsedAt === 0) return t('next.safe.neverUsed');
  const by = secret.usedBy[0]?.title ?? '';
  const ago = formatAgoFull(secret.lastUsedAt, now);
  return by === '' ? t('next.safe.used', { ago }) : t('next.safe.usedBy', { chat: by, ago });
};

interface IRowProps {
  secret: ISecret;
  leaving: boolean;
  now: number;
  actions: ISafeActions;
}

const SecretRow = ({ secret, leaving, now, actions }: IRowProps) => {
  const [replacing, setReplacing] = useState(false);
  const [draft, setDraft] = useState('');
  const [sure, setSure] = useState(false);
  const act = useAction();

  useEffect(() => {
    if (!sure) return;
    const timer = window.setTimeout(() => setSure(false), SURE_MS);
    return () => window.clearTimeout(timer);
  }, [sure]);

  const save = async () => {
    const value = draft;
    if (value.trim() === '') return;
    // The value leaves the page's state the moment it is sent.
    setDraft('');
    if (await act.run('put', () => actions.onPut(secret.name, value))) setReplacing(false);
  };
  const remove = () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    void act.run('remove', () => actions.onRemove(secret.name));
  };
  const cancel = () => {
    setReplacing(false);
    setDraft('');
    act.clear();
  };

  return (
    <div className="hx-reveal hx-safe-wrap" data-open={!leaving}>
      <div className="hx-reveal__in">
        <div className="hx-safe">
          <div className="hx-safe__head">
            <span className="hx-safe__ic">
              <Icon name="key-round" size="sm" />
            </span>
            <span className="hx-safe__text">
              <span className="hx-safe__name">{secret.name}</span>
              <span className="hx-safe__sub">
                {putLine(secret, now)} · {usedLine(secret, now)}
              </span>
            </span>
            <span className="hx-safe__actions">
              <SetButton tone="ghost" disabled={leaving || replacing} onClick={() => setReplacing(true)}>
                {t('next.safe.replace')}
              </SetButton>
              <SetButton
                tone="ghost"
                icon="trash-2"
                busy={act.busy === 'remove'}
                disabled={leaving}
                onClick={remove}
              >
                {sure ? t('next.safe.removeSure') : t('next.safe.remove')}
              </SetButton>
            </span>
          </div>
          <Reveal open={replacing && !leaving}>
            <div className="hx-safe__form">
              <SecretField
                value={draft}
                onChange={(value) => {
                  setDraft(value);
                  act.clear();
                }}
                label={t('next.safe.value', { name: secret.name })}
                placeholder={t('next.safe.value.placeholder')}
                disabled={act.busy !== null}
                invalid={act.error !== null}
                onSubmit={() => void save()}
              />
              <div className="hx-preset__row">
                <SetButton
                  tone="primary"
                  busy={act.busy === 'put'}
                  disabled={draft.trim() === ''}
                  onClick={() => void save()}
                >
                  {t('next.safe.save')}
                </SetButton>
                <SetButton tone="ghost" onClick={cancel}>
                  {t('next.safe.cancel')}
                </SetButton>
              </div>
            </div>
          </Reveal>
          <Reveal open={act.error !== null}>
            <div className="hx-safe__err">
              <FormError>{act.kept}</FormError>
            </div>
          </Reveal>
        </div>
      </div>
    </div>
  );
};

/** «Добавить секрет»: a name and a value. */
const AddSecret = ({ onPut, taken }: { onPut: ISafeActions['onPut']; taken: (name: string) => boolean }) => {
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const act = useAction();
  const field = useRef<HTMLInputElement>(null);
  const ids = useId();
  useEffect(() => {
    if (adding) field.current?.focus();
  }, [adding]);
  const trimmed = name.trim();
  let nameError: string | null = null;
  if (trimmed !== '' && !SECRET_NAME.test(trimmed)) nameError = t('next.safe.reason.bad-name');
  else if (taken(trimmed)) nameError = t('next.safe.taken');
  const ready = trimmed !== '' && nameError === null && value.trim() !== '';

  const add = async () => {
    if (!ready) return;
    const sent = value;
    setValue('');
    if (await act.run('add', () => onPut(trimmed, sent))) {
      setAdding(false);
      setName('');
    }
  };
  const cancel = () => {
    setAdding(false);
    setName('');
    setValue('');
    act.clear();
  };
  return (
    <div className="hx-safe-add">
      <Reveal open={!adding}>
        <button type="button" className="hx-safe-add__btn" onClick={() => setAdding(true)}>
          <Icon name="plus" size="sm" />
          {t('next.safe.add')}
        </button>
      </Reveal>
      <Reveal open={adding}>
        <div className="hx-safe__form hx-safe-add__form">
          <div className="hx-safe__field">
            <label className="hx-safe__label" htmlFor={`${ids}-name`}>
              {t('next.safe.name')}
            </label>
            <input
              id={`${ids}-name`}
              ref={field}
              className="hx-set__input hx-set__input--mono"
              value={name}
              maxLength={64}
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              placeholder={t('next.safe.name.placeholder')}
              aria-invalid={nameError !== null || undefined}
              onChange={(event) => {
                setName(event.target.value);
                act.clear();
              }}
            />
            <span className="hx-safe__hint">{nameError ?? t('next.safe.name.hint')}</span>
          </div>
          <SecretField
            value={value}
            onChange={(next) => {
              setValue(next);
              act.clear();
            }}
            label={t('next.safe.value.field')}
            placeholder={t('next.safe.value.placeholder')}
            disabled={act.busy !== null}
            invalid={act.error !== null}
            onSubmit={() => void add()}
          />
          <div className="hx-preset__row">
            <SetButton tone="primary" busy={act.busy === 'add'} disabled={!ready} onClick={() => void add()}>
              {t('next.safe.save')}
            </SetButton>
            <SetButton tone="ghost" onClick={cancel}>
              {t('next.safe.cancel')}
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

const SafeSection = ({ secrets, failed = null, now, ...actions }: ISafeSectionProps) => {
  const rows = useLeavingList(secrets ?? [], (secret) => secret.name, LEAVE_MS);
  return (
    <Group title={t('next.safe.title')} foot={t('next.safe.foot')}>
      {secrets === null && failed === null && (
        <div className="hx-set__empty">{t('next.settings.loading')}</div>
      )}
      {secrets === null && failed !== null && (
        <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
      )}
      {secrets !== null && (
        <>
          {secrets.length === 0 && <p className="hx-safe-empty">{t('next.safe.empty')}</p>}
          {rows.map(({ item, leaving }) => (
            <SecretRow key={item.name} secret={item} leaving={leaving} now={now} actions={actions} />
          ))}
          <AddSecret onPut={actions.onPut} taken={(name) => secrets.some((s) => s.name === name)} />
        </>
      )}
    </Group>
  );
};

export default SafeSection;
