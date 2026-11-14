// The agent of the Settings window (the design's .isle): the window's one field at the bottom of the page (the host gives
// it, bindings/settings-agent), and over it what the agent answers as it comes — its words with links to the window's
// places, a change it offers with «Поменять / Не надо», and a change made, from what to what, with the way back.
import { answerParts } from '@/next/features/settings/agent-map';
import type { ReactNode } from 'react';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';

export const ISLAND = {
  idle: 'idle',
  thinking: 'thinking',
  /** The words are coming. */
  answering: 'answering',
  done: 'done',
  failed: 'failed',
} as const;
export type TIslandPhase = (typeof ISLAND)[keyof typeof ISLAND];

export interface IIslandChange {
  id: string;
  title: string;
  was: string;
  now: string;
  busy: boolean;
  onUndo: () => void;
}

/** A change the agent offers; nothing changes until the person says so. */
export interface IIslandConfirm {
  title: string;
  was: string;
  now: string;
  busy: boolean;
  onApply: () => void;
  onReject: () => void;
}

/** What the person can do about an answer that could not be given (no model: choose one). */
export interface IIslandAction {
  label: string;
  onClick: () => void;
}

interface IAgentIslandProps {
  phase: TIslandPhase;
  /** What the agent said, or why it did not answer. */
  text: string;
  changes: readonly IIslandChange[];
  confirm: IIslandConfirm | null;
  action: IIslandAction | null;
  /** The card over the field is leaving. */
  closing: boolean;
  /** The field: the window's one. */
  field: ReactNode;
  onDismiss: () => void;
  /** A link of the answer to a place of the window («Настройки → Подключения»): the window goes there. */
  onGo: (path: string) => void;
}

/** The answer with its links to the window's places as jumps there. */
const AnswerText = ({ text, onGo }: { text: string; onGo: (path: string) => void }) => (
  <div className="hx-isle__text">
    {answerParts(text).map((part, i) =>
      part.k === 'link' ? (
        <button key={i} type="button" className="hx-isle__go" onClick={() => onGo(part.path)}>
          {part.text}
          <Icon name="arrow-up-right" size="sm" />
        </button>
      ) : (
        <span key={i}>{part.text}</span>
      ),
    )}
  </div>
);

const AgentIsland = ({
  phase,
  text,
  changes,
  confirm,
  action,
  closing,
  field,
  onDismiss,
  onGo,
}: IAgentIslandProps) => {
  return (
    <div className="hx-isle">
      {phase === ISLAND.thinking && (
        <div className="hx-isle__reply" role="status">
          <span className="hx-isle__think">
            <Icon name="loader-circle" size="sm" />
            {t('next.settings.agentIsle.thinking')}
          </span>
        </div>
      )}
      {(phase === ISLAND.answering || phase === ISLAND.done || phase === ISLAND.failed) && (
        <div className="hx-isle__reply" data-state={closing ? 'closing' : undefined} role="status">
          <AnswerText text={text} onGo={onGo} />
          {changes.map((change) => (
            <div key={change.id} className="hx-isle__diff">
              <span className="hx-isle__diff-title">{change.title}</span>
              <s>{change.was}</s>
              <Icon name="arrow-right" size="sm" />
              <b>{change.now}</b>
              <Button size="sm" variant="ghost" icon="undo-2" busy={change.busy} onClick={change.onUndo}>
                {t('next.settingsChanges.undo')}
              </Button>
            </div>
          ))}
          {confirm && (
            <div className="hx-isle__diff">
              <span className="hx-isle__diff-title">{confirm.title}</span>
              <s>{confirm.was}</s>
              <Icon name="arrow-right" size="sm" />
              <b>{confirm.now}</b>
            </div>
          )}
          {phase !== ISLAND.answering && (
            <div className="hx-isle__acts">
              {confirm ? (
                <>
                  <Button size="sm" variant="primary" busy={confirm.busy} onClick={confirm.onApply}>
                    {t('next.settings.agentIsle.apply')}
                  </Button>
                  <Button size="sm" variant="ghost" disabled={confirm.busy} onClick={confirm.onReject}>
                    {t('next.settings.agentIsle.reject')}
                  </Button>
                </>
              ) : (
                <Button size="sm" variant="ghost" onClick={onDismiss}>
                  {t('next.settings.agentIsle.ok')}
                </Button>
              )}
              {action && (
                <Button size="sm" variant="ghost" onClick={action.onClick}>
                  {action.label}
                </Button>
              )}
            </div>
          )}
        </div>
      )}
      {field}
    </div>
  );
};

export default AgentIsland;
