// «Ждут код» in the chat's column, under its subagents: each command of the chat that waits for a one-time code is a
// card — what the code is for, how long the command still waits, the cells, «Отменить». The last digit sends the
// code; the cells light up and the card folds away. A card opens and folds by height; the section does too.
import { useState } from 'react';
import type { ICodeAsk } from '@/next/features/safe/api';
import { duration } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import CodeField, { CODE_FIELD, type TCodeFieldState } from '@/next/ui/code-field';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresentFlag, useLeavingList } from '@/next/ui/presence';
import '@/next/styles/code-asks.css';

/** How long a card takes to fold away, and how long the lit cells stay before it does. */
export const CODE_EXIT_MS = 220;
const DONE_MS = 520;
const GONE_MS = 1600;

/** What became of a sent code: taken, refused for its digits, gone (the command waits no more), or not sent. */
export const CODE_SENT = { ok: 'ok', bad: 'bad', gone: 'gone', failed: 'failed' } as const;
export type TCodeSent = (typeof CODE_SENT)[keyof typeof CODE_SENT];

interface ICodeAsksProps {
  asks: readonly ICodeAsk[];
  /** This page's clock, ticking. */
  now: number;
  /** The machine's clock minus this page's. */
  skew: number;
  onPut: (ask: ICodeAsk, code: string) => Promise<TCodeSent>;
  onCancel: (ask: ICodeAsk) => Promise<void>;
  /** The card leaves the list: answered or cancelled here. */
  onDone: (ask: ICodeAsk) => void;
}

const NOTE = { none: null, failed: 'next.safe.code.failed', gone: 'next.safe.code.gone' } as const;
type TNote = (typeof NOTE)[keyof typeof NOTE];

const CodeAskCard = ({
  ask,
  left,
  leaving,
  onPut,
  onCancel,
  onDone,
}: {
  ask: ICodeAsk;
  left: number;
  leaving: boolean;
} & Pick<ICodeAsksProps, 'onPut' | 'onCancel' | 'onDone'>) => {
  const [value, setValue] = useState('');
  const [state, setState] = useState<TCodeFieldState>(CODE_FIELD.idle);
  const [note, setNote] = useState<TNote>(NOTE.none);

  const send = async (code: string) => {
    setState(CODE_FIELD.busy);
    setNote(NOTE.none);
    const sent = await onPut(ask, code);
    // The cells never keep a code that left: they show it lit for a moment, then the card folds with them empty.
    if (sent === CODE_SENT.ok) {
      setState(CODE_FIELD.done);
      setTimeout(() => {
        setValue('');
        onDone(ask);
      }, DONE_MS);
      return;
    }
    setValue('');
    setState(CODE_FIELD.invalid);
    if (sent === CODE_SENT.gone) {
      setNote(NOTE.gone);
      setTimeout(() => onDone(ask), GONE_MS);
      return;
    }
    if (sent === CODE_SENT.failed) setNote(NOTE.failed);
  };

  return (
    <div className="hx-codeask" data-leaving={leaving || undefined}>
      <div className="hx-codeask__clip">
        <div className="hx-codeask__body">
          <div className="hx-codeask__top">
            <span className="hx-codeask__why">{ask.why}</span>
            <span className="hx-codeask__left">{t('next.safe.code.left', { time: duration(left) })}</span>
          </div>
          <CodeField
            value={value}
            onChange={(next) => {
              setValue(next);
              if (state === CODE_FIELD.invalid) setState(CODE_FIELD.idle);
            }}
            onComplete={(code) => void send(code)}
            length={ask.digits}
            label={t('next.safe.code.aria', { why: ask.why })}
            state={state}
            disabled={leaving}
          />
          <div className="hx-codeask__foot">
            {note && <span className="hx-codeask__note">{t(note)}</span>}
            <button
              type="button"
              className="hx-codeask__cancel"
              disabled={leaving || state === CODE_FIELD.busy || state === CODE_FIELD.done}
              onClick={() => {
                setValue('');
                void onCancel(ask).finally(() => onDone(ask));
              }}
            >
              {t('next.safe.code.cancel')}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

const CodeAsks = ({ asks, now, skew, onPut, onCancel, onDone }: ICodeAsksProps) => {
  const section = usePresentFlag(asks.length > 0, CODE_EXIT_MS);
  const cards = useLeavingList(asks, (a) => a.id, CODE_EXIT_MS);
  if (!section.mounted) return null;
  return (
    <section className="hx-side__sec hx-codes" data-leaving={section.state === PRESENCE.closing || undefined}>
      <div className="hx-codes__clip">
        <h3 className="hx-codes__head hx-side__ttl">
          <Icon name="key-round" size="sm" />
          {t('next.safe.code.title')}
        </h3>
        {cards.map(({ item, leaving }) => (
          <CodeAskCard
            key={item.id}
            ask={item}
            left={Math.max(item.until - (now + skew), 0)}
            leaving={leaving}
            onPut={onPut}
            onCancel={onCancel}
            onDone={onDone}
          />
        ))}
      </div>
    </section>
  );
};

export default CodeAsks;
