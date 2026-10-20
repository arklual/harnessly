import type { ReactNode } from 'react';
import { MessageSquare } from 'lucide-react';
import { SiOpenai } from 'react-icons/si';
import ClaudeLogo from '@/components/icons/claude-logo';
import ClaudeSpark, { type TSparkState } from '@/components/icons/claude-spark';
import { cn } from '@/lib/utils';

export type TAgentAvatarProvider = 'claude' | 'codex' | 'chat';

const PROVIDER_ICONS: Record<TAgentAvatarProvider, ReactNode> = {
  claude: <ClaudeLogo />,
  codex: <SiOpenai />,
  chat: <MessageSquare />,
};

interface IAgentAvatarProps {
  provider?: TAgentAvatarProvider;
  working?: boolean;
  sparkState?: TSparkState;
  className?: string;
}

export default function AgentAvatar({
  provider = 'claude',
  working = false,
  sparkState = 'thinking',
  className,
}: IAgentAvatarProps) {
  return (
    <span
      className={cn('agent-avatar', `agent-avatar-${provider}`, working && 'agent-avatar-working', className)}
      aria-hidden="true"
    >
      {working && provider === 'claude' ? <ClaudeSpark state={sparkState} /> : PROVIDER_ICONS[provider]}
    </span>
  );
}
