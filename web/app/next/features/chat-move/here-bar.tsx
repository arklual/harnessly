// Over the field of a chat that lives on another machine: where it lives, and «Продолжить здесь». A machine
// that is off says so and offers nothing; a move under way keeps the button busy; a refusal says why in its place.
import { t } from '@/next/i18n';
import '@/next/styles/chat-move.css';

export const HERE_PHASE = {
  idle: 'idle',
  off: 'off',
  moving: 'moving',
  failed: 'failed',
} as const;

export type THerePhase = (typeof HERE_PHASE)[keyof typeof HERE_PHASE];

export interface IHereBarProps {
  /** The machine the chat lives on. */
  machine: string;
  phase: THerePhase;
  /** The refusal, in the gateway's words or ours, while `failed`. */
  reason: string | null;
  onContinue: () => void;
}

const noteOf = ({ machine, phase, reason }: Omit<IHereBarProps, 'onContinue'>): string => {
  if (phase === HERE_PHASE.off) return t('next.moveVm.here.off', { machine });
  if (phase === HERE_PHASE.moving) return t('next.moveVm.here.moving');
  if (phase === HERE_PHASE.failed && reason) return reason;
  return t('next.moveVm.here.on', { machine });
};

const HereBar = (props: IHereBarProps) => {
  const { phase, onContinue } = props;
  const moving = phase === HERE_PHASE.moving;
  return (
    <div className="hx-here" data-phase={phase} role={phase === HERE_PHASE.failed ? 'alert' : 'status'}>
      <span className="hx-here__text">{noteOf(props)}</span>
      {phase !== HERE_PHASE.off && (
        <button
          type="button"
          className="hx-btn hx-here__go"
          data-variant="primary"
          data-busy={moving}
          disabled={moving}
          onClick={onContinue}
        >
          <span>{t('next.moveVm.here.go')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      )}
    </div>
  );
};

export default HereBar;
