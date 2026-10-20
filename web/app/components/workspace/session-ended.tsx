// The agent of this conversation is not running (it exited, or the machine restarted): its session can go on.
import { Play } from 'lucide-react';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import { t } from '@/lib/i18n';

interface ISessionEndedProps {
  provider: 'claude' | 'codex';
  starting: boolean;
  error: string | null;
  canResume: boolean;
  onResume: () => void;
}

export default function SessionEnded({ provider, starting, error, canResume, onResume }: ISessionEndedProps) {
  const name = provider === 'codex' ? 'Codex' : 'Claude';
  return (
    <div className="tw-tui-mirror" role="status" data-testid="session-ended">
      <div className="tw-tui-mirror-head">
        <span className="tw-tui-mirror-eyebrow">{t('workspace.sessionEnded.notRunning', { name })}</span>
        <span className="tw-tui-mirror-title">
          {canResume ? t('workspace.sessionEnded.resumeHint') : t('workspace.sessionEnded.noSession')}
        </span>
      </div>
      {error && <p className="tw-tui-mirror-error">{error}</p>}
      {canResume && (
        <div className="tw-tui-mirror-foot">
          <TwButton
            size={BUTTON_SIZE.sm}
            variant={BUTTON_VARIANT.primary}
            icon={<Play />}
            pending={starting}
            onClick={onResume}
          >
            {t('workspace.sessionEnded.resume')}
          </TwButton>
        </div>
      )}
    </div>
  );
}
