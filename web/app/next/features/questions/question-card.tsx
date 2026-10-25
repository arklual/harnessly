// The agent's questions over the field (as in Codex): one card, a step per question with tabs, one
// or several choices, a text preview beside the options, digits 1–9 to pick, and the person's own answer typed in the
// field below. «Answer in chat» folds the card into a one-line bar (a morph of one card) and a click unfolds it.
// Sending waits for the agent: the button turns busy, a failure keeps the choices and offers Retry. Answered on
// another device, the card leaves and the page says so quietly.
import { useEffect, useRef, useState } from 'react';
import type { AnswerItem, Question } from '@/next/features/questions/types';
import useDigitKeys from '@/next/features/questions/use-digit-keys';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/ui.css';
import '@/next/styles/questions.css';

export const ASK_EXIT_MS = 260;
/** A single choice without a preview moves on by itself after this (as in Codex). */
const AUTO_NEXT_MS = 260;
const MORPH_MS = 560;

interface IQuestionCardProps {
  /** The open question; null once there is none (the card leaves). */
  question: Question | null;
  /** This device's id: an answer from another one is announced, not ours. */
  device: string;
  /** The person's own answer in the field, for the current step. */
  ownAnswer: string;
  onAnswer: (questionId: string, answers: AnswerItem[]) => Promise<unknown>;
  /** The card is folded into its bar, so the person writes in the chat. */
  onFold?: (folded: boolean) => void;
}

type TSelection = Readonly<Record<number, readonly number[]>>;

/** The card's own state: the step, the choices, folded or not. Reset when another question comes. */
const useAskState = (id: string | null) => {
  const [state, setState] = useState({ id, step: 0, prev: 0, sel: {} as TSelection, folded: false });
  if (state.id !== id) setState({ id, step: 0, prev: 0, sel: {}, folded: false });
  return [state, setState] as const;
};

/** Folding and unfolding animate the two halves' heights in px together, which a grid-rows transition did with a jerk. */
const morph = (root: HTMLElement | null, apply: () => void) => {
  const full = root?.querySelector<HTMLElement>('.hx-ask__full');
  const fold = root?.querySelector<HTMLElement>('.hx-ask__fold');
  if (!full || !fold) {
    apply();
    return;
  }
  full.style.height = `${full.getBoundingClientRect().height}px`;
  fold.style.height = `${fold.getBoundingClientRect().height}px`;
  void full.offsetHeight;
  apply();
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      const folded = root?.dataset.min === 'true';
      full.style.height = `${folded ? 0 : (full.firstElementChild?.scrollHeight ?? 0)}px`;
      fold.style.height = `${folded ? (fold.firstElementChild?.scrollHeight ?? 0) : 0}px`;
      setTimeout(() => {
        full.style.height = '';
        fold.style.height = '';
      }, MORPH_MS + 40);
    }),
  );
};

const QuestionCard = ({ question, device, ownAnswer, onAnswer, onFold }: IQuestionCardProps) => {
  const open = question !== null && (question.state.k === 'open' || question.state.k === 'answering');
  const { value: shown, state: presence } = usePresence(open ? question : null, ASK_EXIT_MS);
  const [ask, setAsk] = useAskState(shown?.id ?? null);
  const send = usePending<'send'>();
  const rootRef = useRef<HTMLDivElement>(null);
  const autoNext = useRef<ReturnType<typeof setTimeout> | null>(null);
  const answeredAt = question?.state.k === 'answered' ? question.state.at : null;
  const answeredBy = question?.state.k === 'answered' ? question.state.byDevice : null;

  useEffect(() => () => clearTimeout(autoNext.current ?? undefined), []);

  // Answered somewhere else: the card leaves by itself; a quiet notice says where. The draft in the field stays.
  useEffect(() => {
    if (answeredAt === null || answeredBy === null || answeredBy === device) return;
    toast(t('next.ask.answeredElsewhere', { device: answeredBy }));
  }, [answeredAt, answeredBy, device]);

  const items = shown?.items ?? [];
  const current = items[ask.step];
  const busy = isBusy(send.phase) || shown?.state.k === 'answering';
  const own = ownAnswer.trim();
  const answered = (index: number) =>
    (ask.sel[index]?.length ?? 0) > 0 || (index === ask.step && own.length > 0);
  const last = ask.step === items.length - 1;
  const allAnswered = items.every((_, index) => answered(index));
  const hasPreview = current?.options.some((option) => option.preview) ?? false;
  const needsButton = Boolean(current?.multi) || hasPreview || last;

  const go = (step: number) => setAsk((state) => ({ ...state, step, prev: state.step }));

  const submit = () => {
    if (!shown) return;
    const answers: AnswerItem[] = items.map((_, index) => {
      const choices = [...(ask.sel[index] ?? [])];
      if (index === ask.step && own && choices.length === 0) return { i: index, choices, text: own };
      return { i: index, choices };
    });
    send.run('send', () => onAnswer(shown.id, answers));
  };

  const advance = (sel: TSelection) => {
    const next = items.findIndex((_, index) => index > ask.step && (sel[index]?.length ?? 0) === 0);
    if (next >= 0) go(next);
    else if (items.every((_, index) => (sel[index]?.length ?? 0) > 0)) submit();
  };

  const pick = (option: number) => {
    if (!current || busy) return;
    const was = ask.sel[ask.step] ?? [];
    let chosen: number[] = [option];
    if (current.multi) chosen = was.includes(option) ? was.filter((o) => o !== option) : [...was, option];
    const sel = { ...ask.sel, [ask.step]: chosen };
    setAsk((state) => ({ ...state, sel }));
    if (!current.multi && !hasPreview && !last) {
      clearTimeout(autoNext.current ?? undefined);
      autoNext.current = setTimeout(() => advance(sel), AUTO_NEXT_MS);
    }
  };

  const fold = (folded: boolean) => {
    morph(rootRef.current, () => setAsk((state) => ({ ...state, folded })));
    onFold?.(folded);
  };

  useDigitKeys(open && !ask.folded && !busy, current?.options.length ?? 0, (n) => pick(n - 1));

  if (!shown || !current) return null;

  let hint = t('next.ask.ownHint');
  if (own) hint = t('next.ask.ownAnswer', { text: own });
  const nextLabel = last && allAnswered ? t('next.ask.send') : t('next.ask.next');

  return (
    <div
      ref={rootRef}
      className="hx-ask"
      data-state={presence === PRESENCE.closing ? 'closing' : 'open'}
      data-min={String(ask.folded)}
      data-sending={busy || undefined}
      role="dialog"
      aria-label={t('next.ask.label')}
    >
      <div className="hx-ask__card">
        <div className="hx-ask__inner">
          <div className="hx-ask__fold">
            <div className="hx-ask__foldclip">
              <button
                type="button"
                className="hx-ask-bar"
                tabIndex={ask.folded ? 0 : -1}
                onClick={() => fold(false)}
                title={t('next.ask.unfold')}
              >
                <Icon name="message-square-text" size="sm" />
                <span className="hx-ask-bar__q">{current.question}</span>
                {items.length > 1 && (
                  <span className="hx-ask-bar__count">
                    {t('next.ask.stepOf', { n: ask.step + 1, total: items.length })}
                  </span>
                )}
                <Icon name="chevron-up" size="sm" className="hx-ask-bar__chev" />
              </button>
            </div>
          </div>
          <div className="hx-ask__full">
            <div className="hx-ask__fullclip">
              <div className="hx-ask__head">
                {items.length > 1 && (
                  <div className="hx-ask__tabs" role="tablist">
                    {items.map((item, index) => (
                      <button
                        key={index}
                        type="button"
                        role="tab"
                        className="hx-ask__tab"
                        aria-selected={index === ask.step}
                        aria-current={index === ask.step}
                        data-done={answered(index) || undefined}
                        onClick={() => go(index)}
                      >
                        <Icon name="check" size="sm" />
                        {item.header}
                      </button>
                    ))}
                  </div>
                )}
                <span className="hx-spacer" />
                <button
                  type="button"
                  className="hx-ask__min"
                  onClick={() => fold(true)}
                  title={t('next.ask.foldTip')}
                >
                  {t('next.ask.inChat')}
                  <Icon name="chevron-down" size="sm" />
                </button>
              </div>
              <div className="hx-ask__panes">
                {items.map((item, index) => {
                  let dir = 'cur';
                  if (index < ask.step) dir = 'prev';
                  if (index > ask.step) dir = 'next';
                  const sel = ask.sel[index] ?? [];
                  const preview = item.options[sel[0] ?? 0]?.preview ?? null;
                  const active = index === ask.step;
                  return (
                    <div
                      key={index}
                      className="hx-ask__pane"
                      data-active={String(active)}
                      data-dir={dir}
                      aria-hidden={!active}
                    >
                      <div className="hx-ask__q">{item.question}</div>
                      <div className="hx-ask__body" data-preview={Boolean(preview) || undefined}>
                        <div className="hx-ask__opts" role={item.multi ? 'group' : 'radiogroup'}>
                          {item.options.map((option, optionIndex) => (
                            <button
                              key={optionIndex}
                              type="button"
                              className="hx-ask__opt"
                              aria-pressed={sel.includes(optionIndex)}
                              data-multi={item.multi || undefined}
                              tabIndex={active ? 0 : -1}
                              onClick={() => pick(optionIndex)}
                            >
                              <span className="hx-ask__key">{optionIndex + 1}</span>
                              <span className="hx-ask__text">
                                <span className="hx-ask__label">{option.label}</span>
                                {option.description && (
                                  <span className="hx-ask__desc">{option.description}</span>
                                )}
                              </span>
                              <span className="hx-ask__mark">
                                <Icon name="check" size="sm" />
                              </span>
                            </button>
                          ))}
                        </div>
                        {preview && <pre className="hx-ask__preview">{preview}</pre>}
                      </div>
                    </div>
                  );
                })}
              </div>
              <div className="hx-ask__foot">
                {send.phase === PENDING.failed ? (
                  <span className="hx-ask__err" role="alert">
                    <Icon name="circle-alert" size="sm" />
                    {t('next.ask.failed')}
                    <button type="button" className="hx-msg-user__act" onClick={send.retry}>
                      {t('next.ask.retry')}
                    </button>
                  </span>
                ) : (
                  <span className="hx-ask__hint">
                    {send.phase === PENDING.slow ? t('next.ask.slow') : hint}
                  </span>
                )}
                <span className="hx-spacer" />
                {ask.step > 0 && (
                  <Button size="sm" variant="ghost" disabled={busy} onClick={() => go(ask.step - 1)}>
                    {t('next.ask.back')}
                  </Button>
                )}
                {needsButton && (
                  <Button
                    size="sm"
                    variant="primary"
                    busy={busy}
                    disabled={!answered(ask.step)}
                    onClick={() => (last && allAnswered ? submit() : advance(ask.sel))}
                  >
                    {nextLabel}
                  </Button>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default QuestionCard;
