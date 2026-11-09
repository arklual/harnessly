// What waits for the person, inside its row of the notification centre: the question itself, the command a tool
// wants to run, the plan — and the answer right there. A question opens in place with its options (1–9 from the
// keyboard) and a field for an answer of one's own; a permission is allowed or denied with one tap; what has no answer
// here (a plan, several questions) opens its chat. The answer goes through the same commands as the chat's own cards.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import usePendingList from '@/next/ui/use-pending-list';

export type TWaitDetail =
  | { k: 'loading' }
  | { k: 'question'; text: string; options: readonly string[] }
  | { k: 'permission'; command: string }
  | { k: 'plain'; text: string };

export interface IWaitRowProps {
  detail: TWaitDetail;
  open: boolean;
  onToggle: () => void;
  onOpenChat: () => void;
  onChoose?: (index: number) => Promise<unknown>;
  onText?: (text: string) => Promise<unknown>;
  onDecide?: (allow: boolean) => Promise<unknown>;
}

const WaitRow = ({ detail, open, onToggle, onOpenChat, onChoose, onText, onDecide }: IWaitRowProps) => {
  const { pending, run } = usePendingList();
  const [text, setText] = useState('');
  const waitRef = useRef<HTMLDivElement>(null);
  const cmdRef = useRef<HTMLSpanElement>(null);
  // A command is allowed only once it is seen whole: cut to three lines, «Разрешить» waits until it is opened.
  const [full, setFull] = useState(false);
  const [clamped, setClamped] = useState(false);
  const command = detail.k === 'permission' ? detail.command : null;
  useEffect(() => {
    const el = cmdRef.current;
    if (!el || full) return;
    const measure = () => setClamped(el.scrollHeight > el.clientHeight + 1);
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [command, full]);
  const asking = open && detail.k === 'question';
  // Opened, the question takes the focus (its first option): 1–9 answer, Esc folds; and it is scrolled into sight.
  useEffect(() => {
    const el = waitRef.current;
    if (!asking || !el) return;
    el.querySelector<HTMLElement>('button, input')?.focus({ preventScroll: true });
    el.closest('.hx-nc__row')?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [asking]);
  const busy = pending.size > 0;
  const failed = t('next.nc.answerFailed');

  if (detail.k === 'loading')
    return (
      <span className="hx-nc__gist" aria-busy="true">
        <span className="hx-sk hx-nc__sk-line" />
      </span>
    );

  if (detail.k === 'permission')
    return (
      <>
        <button
          type="button"
          className="hx-nc__cmd"
          data-full={full || undefined}
          aria-expanded={full}
          title={full ? undefined : t('next.nc.showWhole')}
          onClick={() => setFull((was) => !was)}
        >
          <span ref={cmdRef} className="hx-nc__cmd-text">
            {detail.command}
          </span>
        </button>
        {clamped && !full && <span className="hx-nc__cmd-hint">{t('next.nc.seeWhole')}</span>}
        <span className="hx-nc__acts">
          <button
            type="button"
            className="hx-nc__btn"
            data-busy={pending.has('allow') || undefined}
            disabled={busy || !onDecide || (clamped && !full)}
            onClick={() => onDecide && void run('allow', failed, () => onDecide(true))}
          >
            {t('next.nc.allow')}
          </button>
          <button
            type="button"
            className="hx-nc__btn"
            data-soft=""
            data-busy={pending.has('deny') || undefined}
            disabled={busy || !onDecide}
            onClick={() => onDecide && void run('deny', failed, () => onDecide(false))}
          >
            {t('next.nc.deny')}
          </button>
        </span>
      </>
    );

  if (detail.k === 'plain')
    return (
      <>
        <span className="hx-nc__gist">{detail.text}</span>
        <span className="hx-nc__acts">
          <button type="button" className="hx-nc__btn" onClick={onOpenChat}>
            {t('next.nc.openChat')}
          </button>
        </span>
      </>
    );

  if (!open)
    return (
      <>
        <span className="hx-nc__gist">{detail.text}</span>
        <span className="hx-nc__acts">
          <button type="button" className="hx-nc__btn" aria-expanded={false} onClick={onToggle}>
            {t('next.nc.answer')}
          </button>
        </span>
      </>
    );

  const send = () => {
    const said = text.trim();
    if (!said || !onText) return;
    void run('text', failed, async () => {
      await onText(said);
      setText('');
    });
  };
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target instanceof HTMLInputElement) return;
    if (e.key === 'Escape') {
      e.stopPropagation();
      onToggle();
      return;
    }
    const n = Number(e.key);
    if (Number.isInteger(n) && n >= 1 && n <= detail.options.length && onChoose && !busy) {
      e.preventDefault();
      void run(`opt:${n - 1}`, failed, () => onChoose(n - 1));
    }
  };

  return (
    <div ref={waitRef} className="hx-nc__wait" data-no-swipe="" onKeyDown={onKey}>
      <p className="hx-nc__q">{detail.text}</p>
      {detail.options.map((label, i) => (
        <button
          key={label}
          type="button"
          className="hx-nc__opt"
          data-busy={pending.has(`opt:${i}`) || undefined}
          disabled={busy || !onChoose}
          onClick={() => onChoose && void run(`opt:${i}`, failed, () => onChoose(i))}
        >
          {i < 9 && <kbd>{i + 1}</kbd>}
          <span>{label}</span>
        </button>
      ))}
      <form
        className="hx-nc__field"
        onSubmit={(e) => {
          e.preventDefault();
          send();
        }}
      >
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={t('next.nc.ownAnswer')}
          aria-label={t('next.nc.ownAnswer')}
          disabled={busy || !onText}
          onKeyDown={(e) => {
            if (e.key !== 'Escape') return;
            e.stopPropagation();
            onToggle();
          }}
        />
        <button
          type="submit"
          className="hx-nc__send"
          aria-label={t('next.nc.send')}
          data-busy={pending.has('text') || undefined}
          disabled={busy || !text.trim()}
        >
          <Icon name="arrow-up" size="sm" />
        </button>
      </form>
    </div>
  );
};

export default WaitRow;
