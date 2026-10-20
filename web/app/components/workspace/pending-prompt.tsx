// What the agent waits for above the composer. Claude's questions (AskUserQuestion) become a card with their options
// and a free answer, sent as keys by the runtime (/api/tmux/answer-question). Any other menu on the agent's screen
// (a permission, a plan to approve) is mirrored with its numbered options as buttons.
import { useEffect, useMemo, useState } from 'react';
import { MessageCircleQuestion } from 'lucide-react';
import { toast } from 'sonner';
import TuiMenuMirror from '@/components/agents/tui-menu-mirror';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import {
  EMPTY_CHOICE,
  isChoiceComplete,
  questionShape,
  takesFreeText,
  type IQuestionChoice,
} from '@/lib/ask-question';
import { appFetch } from '@/lib/app-path';
import type { TMirrorState } from '@/hooks/use-slash-commands';
import type { IMirrorScreen } from '@/lib/hl-command-run';
import type { ITimelineAskUserQuestion, ITimelineEntry } from '@/types/timeline';
import { t } from '@/lib/i18n';

/** The question the agent asked last and has no answer yet. */
export const pendingQuestionOf = (entries: readonly ITimelineEntry[]): ITimelineAskUserQuestion | null => {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const entry = entries[i]!;
    if (entry.type === 'user-message' || entry.type === 'turn-end') return null;
    if (entry.type === 'ask-user-question')
      return entry.status === 'pending' && !entry.answers ? entry : null;
  }
  return null;
};

const AskQuestionCard = ({
  entry,
  sessionName,
}: {
  entry: ITimelineAskUserQuestion;
  sessionName: string;
}) => {
  const [step, setStep] = useState(0);
  const [choices, setChoices] = useState<IQuestionChoice[]>(() => entry.questions.map(() => EMPTY_CHOICE));
  const [otherOn, setOtherOn] = useState<boolean[]>(() => entry.questions.map(() => false));
  const [sending, setSending] = useState(false);
  const question = entry.questions[step]!;
  const shape = questionShape(question);
  const choice = choices[step]!;
  const last = step === entry.questions.length - 1;
  const complete = entry.questions.every((_q, i) => isChoiceComplete(choices[i]!, otherOn[i]!));

  const setChoice = (next: IQuestionChoice) =>
    setChoices((all) => all.map((c, i) => (i === step ? next : c)));
  const toggle = (index: number) => {
    setOtherOn((all) => all.map((on, i) => (i === step ? false : on)));
    if (question.multiSelect) {
      const picked = choice.picked.includes(index)
        ? choice.picked.filter((p) => p !== index)
        : [...choice.picked, index];
      setChoice({ picked, other: '' });
    } else {
      setChoice({ picked: [index], other: '' });
      if (!last) setStep(step + 1);
    }
  };

  const send = async () => {
    if (!complete || sending) return;
    setSending(true);
    try {
      const response = await appFetch('/api/tmux/answer-question', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          session: sessionName,
          questions: entry.questions.map(questionShape),
          choices: choices.map((c, i) =>
            otherOn[i] ? { picked: [], other: c.other } : { picked: c.picked, other: '' },
          ),
        }),
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${response.status}`);
      }
    } catch (failure) {
      toast.error(t('workspace.pendingPrompt.answerFailed'), {
        description: failure instanceof Error ? failure.message : undefined,
      });
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="tw-tui-mirror" role="group" aria-label={t('workspace.pendingPrompt.agentQuestion')}>
      <div className="tw-tui-mirror-head">
        <span className="tw-tui-mirror-eyebrow">
          <MessageCircleQuestion size={13} /> {question.header || t('workspace.pendingPrompt.agentQuestion')}
          {entry.questions.length > 1
            ? ` · ${t('workspace.pendingPrompt.stepOf', { step: step + 1, total: entry.questions.length })}`
            : ''}
        </span>
        <span className="tw-tui-mirror-title">{question.question}</span>
      </div>
      <div className="tw-tui-mirror-options">
        {question.options.map((option, index) => {
          const on = !otherOn[step] && choice.picked.includes(index);
          return (
            <TwButton
              key={option.label}
              size={BUTTON_SIZE.sm}
              variant={on ? BUTTON_VARIANT.primary : BUTTON_VARIANT.secondary}
              className="tw-tui-mirror-option"
              aria-pressed={on}
              title={option.description || undefined}
              onClick={() => toggle(index)}
            >
              <span className="tw-tui-mirror-number">{index + 1}</span>
              <span className="tw-tui-mirror-label">{option.label}</span>
            </TwButton>
          );
        })}
        {takesFreeText(shape) && (
          <input
            className="tw-askq-other"
            placeholder={t('workspace.pendingPrompt.ownAnswer')}
            value={otherOn[step] ? choice.other : ''}
            onChange={(event) => {
              const text = event.target.value;
              setOtherOn((all) => all.map((on, i) => (i === step ? text.length > 0 : on)));
              setChoice({ picked: [], other: text });
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.nativeEvent.isComposing) {
                event.preventDefault();
                if (last) void send();
                else setStep(step + 1);
              }
            }}
          />
        )}
      </div>
      <div className="tw-tui-mirror-foot">
        {step > 0 && (
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.ghost} onClick={() => setStep(step - 1)}>
            {t('workspace.pendingPrompt.back')}
          </TwButton>
        )}
        {!last ? (
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.secondary}
            disabled={!isChoiceComplete(choice, otherOn[step]!)}
            onClick={() => setStep(step + 1)}
          >
            {t('workspace.pendingPrompt.next')}
          </TwButton>
        ) : (
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            disabled={!complete}
            pending={sending}
            onClick={() => void send()}
          >
            {t('workspace.pendingPrompt.answer')}
          </TwButton>
        )}
      </div>
    </div>
  );
};

/** A menu on the agent's screen (permission, plan approval), read once when the agent starts waiting. */
const useScreenMenu = (
  sessionName: string,
  waiting: boolean,
  epoch: number,
): [TMirrorState | null, () => void] => {
  const [mirror, setMirror] = useState<TMirrorState | null>(null);
  useEffect(() => {
    if (!waiting) return;
    const controller = new AbortController();
    void appFetch(`/api/platform/commands/screen?session=${encodeURIComponent(sessionName)}`, {
      signal: controller.signal,
      cache: 'no-store',
    })
      .then((response) => (response.ok ? (response.json() as Promise<IMirrorScreen>) : null))
      .then((screen) => {
        if (screen?.menu)
          setMirror({
            kind: 'menu',
            command: screen.menu.title ?? t('workspace.pendingPrompt.agentWaiting'),
            menu: screen.menu,
          });
      })
      .catch(() => {});
    return () => controller.abort();
  }, [sessionName, waiting, epoch]);
  return [waiting ? mirror : null, () => setMirror(null)];
};

interface IPendingPromptProps {
  sessionName: string;
  provider: 'claude' | 'codex';
  waiting: boolean;
  entries: readonly ITimelineEntry[];
  onOpenTerminal: () => void;
}

export default function PendingPrompt({
  sessionName,
  provider,
  waiting,
  entries,
  onOpenTerminal,
}: IPendingPromptProps) {
  const question = useMemo(() => (waiting ? pendingQuestionOf(entries) : null), [waiting, entries]);
  const [epoch, setEpoch] = useState(0);
  const [mirror, clear] = useScreenMenu(sessionName, waiting && !question, epoch);
  if (question) return <AskQuestionCard key={question.id} entry={question} sessionName={sessionName} />;
  if (!mirror) return null;
  const close = () => {
    clear();
    setEpoch((n) => n + 1);
  };
  return (
    <TuiMenuMirror
      mirror={mirror}
      sessionName={sessionName}
      provider={provider}
      onClose={close}
      onOpenTerminal={onOpenTerminal}
    />
  );
}
