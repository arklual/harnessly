import { MessageCircle, SquareTerminal } from 'lucide-react';
import ClaudeLogo from '@/components/icons/claude-logo';
import ClaudeSpark from '@/components/icons/claude-spark';
import OpenAIIcon from '@/components/icons/openai-icon';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

/**
 * The sidebar model of Claude Desktop (`sidebarShownState` and the claude.ai session tile):
 * `ready` is a finished turn nobody opened yet, `done` one that was seen, `failed` a turn that ended on an error.
 * `gone` is ours: the agent process exited or was killed, the conversation stays.
 */
export type TProviderStatus = 'idle' | 'working' | 'needs-input' | 'ready' | 'done' | 'failed' | 'gone';

export const PROVIDER_STATUSES: readonly TProviderStatus[] = [
  'idle',
  'working',
  'needs-input',
  'ready',
  'done',
  'failed',
  'gone',
];

export const PROVIDER_LABELS: Record<string, string> = {
  chat: t('agents.providerIcon.chat'),
  claude: 'Claude',
  codex: 'Codex',
  grok: 'Grok',
  terminal: t('agents.providerIcon.terminal'),
};

const STATUS_LABELS: Record<TProviderStatus, string> = {
  idle: '',
  working: t('agents.providerIcon.working'),
  'needs-input': t('agents.providerIcon.needsInput'),
  ready: t('agents.providerIcon.ready'),
  done: '',
  failed: t('agents.providerIcon.failed'),
  gone: '',
};

const DOT_STATUSES: ReadonlySet<TProviderStatus> = new Set(['needs-input', 'ready', 'failed']);

interface IProviderIconProps {
  provider: string;
  status?: TProviderStatus;
  size?: 'sm' | 'md' | 'lg';
  /** Off where a richer tooltip describes the host (the collapsed rail): no second, native one. */
  native?: boolean;
  className?: string;
}

const Glyph = ({ provider, working }: { provider: string; working: boolean }) => {
  if (provider === 'claude') return working ? <ClaudeSpark state="writing" /> : <ClaudeLogo />;
  if (provider === 'codex') return <OpenAIIcon />;
  if (provider === 'chat') return <MessageCircle />;
  if (provider === 'grok') return <span className="tw-provider-x">𝕏</span>;
  return <SquareTerminal />;
};

const ProviderIcon = ({
  provider,
  status = 'idle',
  size = 'md',
  native = true,
  className,
}: IProviderIconProps) => {
  const label = [PROVIDER_LABELS[provider] ?? provider, STATUS_LABELS[status]].filter(Boolean).join(' · ');
  return (
    <span
      className={cn(
        'tw-provider',
        `tw-provider-${provider}`,
        `tw-provider-${size}`,
        status === 'working' && provider !== 'claude' && 'tw-provider-working',
        status === 'working' && provider === 'claude' && 'tw-provider-sparking',
        className,
      )}
      role="img"
      aria-label={label}
      title={native ? label : undefined}
    >
      <Glyph provider={provider} working={status === 'working'} />
      {DOT_STATUSES.has(status) && <span className={cn('tw-provider-dot', `tw-provider-dot-${status}`)} />}
    </span>
  );
};

export default ProviderIcon;
