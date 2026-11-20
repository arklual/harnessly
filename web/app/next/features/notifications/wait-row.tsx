// What waits for the person, in its card or its row of the notification centre: what exactly is asked, short, and the
// answer right there. One shape for every kind of gate, the same actions as the gate's card in the chat:
//   a permission — the command in one or two lines (monospace) with «Разрешить» / «Запретить»;
//   a question or a menu of the agent's screen (a dialog) — its words and the options as buttons;
//   a plan — its first lines («Показать полностью» opens it here), «Утвердить» / «Доработать».
// «Открыть чат» is one quiet link at the end of the actions, never a button beside them. The container says the kind of
// the wait once (`label`), so nothing is said twice. The answer goes through the same commands as the chat's own card.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import Md from '@/next/features/chat/md';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import usePendingList from '@/next/ui/use-pending-list';

export interface IWaitOption {
  id: string;
  label: string;
}

export type TWaitDetail =
  | { k: 'loading' }
  | { k: 'question'; text: string; options: readonly string[] }
  | { k: 'permission'; command: string }
  /** A menu of the agent's own screen: its words and its options, each one a button. */
  | { k: 'choice'; text: string; options: readonly IWaitOption[] }
  /** A plan: its text; `approve` and `revise` are the options of the gate (null when the agent has not offered one). */
  | { k: 'plan'; md: string; approve: string | null; revise: string | null }
  /** A finished turn of a chat: the start of the answer, and a field to write back at once. */
  | { k: 'reply'; text: string }
  | { k: 'plain'; text: string };

export interface IWaitRowProps {
  detail: TWaitDetail;
  /** What kind of wait it is, in words («Ждёт разрешения»): said here when the container does not say it. */
  label?: string;
  onOpenChat: () => void;
  onChoose?: (index: number) => Promise<unknown>;
  onText?: (text: string) => Promise<unknown>;
  onDecide?: (allow: boolean) => Promise<unknown>;
  /** One option of a menu or a plan, by its id. */
  onPick?: (optionId: string) => Promise<unknown>;
}

/** A plan in a few plain lines: its title and what follows, without the marks of the markup. */
export const planGist = (md: string): string =>
  md
    .split('\n')
    .map((line) =>
      line
        .replace(/^\s{0,3}(#{1,6}|[-*+]|\d+[.)])\s+/, '')
        .replace(/[*_`]/g, '')
        .trim(),
    )
    .filter((line) => line !== '')
    .slice(0, 4)
    .join('\n');

const WaitRow = ({ detail, label, onOpenChat, onChoose, onText, onDecide, onPick }: IWaitRowProps) => {
  const { pending, run } = usePendingList();
  const [text, setText] = useState('');
  const [full, setFull] = useState(false);
  const waitRef = useRef<HTMLDivElement>(null);
  const cmdRef = useRef<HTMLSpanElement>(null);
  const [clamped, setClamped] = useState(false);
  const command = detail.k === 'permission' ? detail.command : null;
  // A command is allowed only once it is seen whole: cut to two lines, «Разрешить» waits until it is opened.
  useEffect(() => {
    const el = cmdRef.current;
    if (!el || full) return;
    const measure = () => setClamped(el.scrollHeight > el.clientHeight + 1);
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [command, full]);
  const busy = pending.size > 0;
  const failed = t('next.nc.answerFailed');

  const kind = label ? <span className="hx-nc__kind">{label}</span> : null;
  if (detail.k === 'loading')
    return (
      <div className="hx-nc__wait" aria-busy="true">
        {kind}
        <span className="hx-sk hx-nc__sk-line" />
      </div>
    );

  const link = (
    <button type="button" className="hx-nc__link" onClick={onOpenChat}>
      {t('next.nc.openChat')}
    </button>
  );
  const pick = (id: string) => onPick && void run(`pick:${id}`, failed, () => onPick(id));

  if (detail.k === 'plain')
    return (
      <div className="hx-nc__wait" data-no-swipe="">
        {kind}
        {detail.text && <span className="hx-nc__essence">{detail.text}</span>}
        <span className="hx-nc__acts">{link}</span>
      </div>
    );

  if (detail.k === 'permission')
    return (
      <div className="hx-nc__wait" data-no-swipe="">
        {kind}
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
        {clamped && !full && (
          <button type="button" className="hx-nc__more" onClick={() => setFull(true)}>
            {t('next.nc.showWhole')}
          </button>
        )}
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
          {link}
        </span>
      </div>
    );

  if (detail.k === 'plan') {
    const gist = planGist(detail.md);
    const approve = detail.approve;
    const revise = detail.revise;
    return (
      <div className="hx-nc__wait" data-no-swipe="">
        {kind}
        {full ? (
          <div className="hx-nc__plan">
            <Md md={detail.md} />
          </div>
        ) : (
          gist && <span className="hx-nc__essence hx-nc__essence--plan">{gist}</span>
        )}
        {detail.md.trim() !== '' && (
          <button
            type="button"
            className="hx-nc__more"
            aria-expanded={full}
            onClick={() => setFull((was) => !was)}
          >
            {full ? t('next.nc.fold') : t('next.nc.showFull')}
          </button>
        )}
        <span className="hx-nc__acts">
          {approve && (
            <button
              type="button"
              className="hx-nc__btn"
              data-busy={pending.has(`pick:${approve}`) || undefined}
              disabled={busy || !onPick}
              onClick={() => pick(approve)}
            >
              {t('next.nc.approve')}
            </button>
          )}
          {revise && (
            <button
              type="button"
              className="hx-nc__btn"
              data-soft=""
              data-busy={pending.has(`pick:${revise}`) || undefined}
              disabled={busy || !onPick}
              onClick={() => pick(revise)}
            >
              {t('next.gate.revise')}
            </button>
          )}
          {link}
        </span>
      </div>
    );
  }

  const send = () => {
    const said = text.trim();
    if (!said || !onText) return;
    void run('text', failed, async () => {
      await onText(said);
      setText('');
    });
  };
  const options: readonly string[] =
    detail.k === 'reply'
      ? []
      : detail.k === 'question'
        ? detail.options
        : detail.options.map((option) => option.label);
  const choose = (index: number) => {
    if (detail.k === 'reply') return;
    if (detail.k === 'question') return onChoose && void run(`opt:${index}`, failed, () => onChoose(index));
    const option = detail.options[index];
    return option && pick(option.id);
  };
  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.target instanceof HTMLInputElement) return;
    const n = Number(e.key);
    if (Number.isInteger(n) && n >= 1 && n <= options.length && !busy) {
      e.preventDefault();
      choose(n - 1);
    }
  };
  const idOf = (index: number) =>
    detail.k === 'question'
      ? `opt:${index}`
      : `pick:${detail.k === 'choice' ? (detail.options[index]?.id ?? '') : ''}`;
  const canAnswer = detail.k === 'question' ? Boolean(onChoose) : Boolean(onPick);
  const writes = detail.k === 'question' || detail.k === 'reply';

  return (
    <div ref={waitRef} className="hx-nc__wait" data-no-swipe="" onKeyDown={onKey}>
      {kind}
      {detail.text &&
        (detail.k === 'reply' ? (
          <span className="hx-nc__essence">{detail.text}</span>
        ) : (
          <p className="hx-nc__q">{detail.text}</p>
        ))}
      {options.map((optionLabel, i) => (
        <button
          key={`${i}:${optionLabel}`}
          type="button"
          className="hx-nc__opt"
          data-busy={pending.has(idOf(i)) || undefined}
          disabled={busy || !canAnswer}
          onClick={() => choose(i)}
        >
          {i < 9 && <kbd>{i + 1}</kbd>}
          <span>{optionLabel}</span>
        </button>
      ))}
      {writes && (
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
            placeholder={t(detail.k === 'reply' ? 'next.nc.reply' : 'next.nc.ownAnswer')}
            aria-label={t(detail.k === 'reply' ? 'next.nc.reply' : 'next.nc.ownAnswer')}
            disabled={busy || !onText}
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
      )}
      <span className="hx-nc__acts">{link}</span>
    </div>
  );
};

export default WaitRow;
