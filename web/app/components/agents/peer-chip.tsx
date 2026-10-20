import ProviderIcon from '@/components/agents/provider-icon';
import usePeers, { findPeer } from '@/hooks/use-peers';
import { cleanConversationTitle } from '@/lib/hl-conversations';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

const STATUS: Record<string, { dot: string; label: string }> = {
  busy: { dot: 'working', label: t('agents.peerChip.busy') },
  idle: { dot: 'idle', label: t('agents.peerChip.idle') },
  waiting: { dot: 'needs-input', label: t('agents.peerChip.waiting') },
};

/** Another agent session as a link to its conversation here: title, state, one click to open it. */
const PeerChip = ({ name, from }: { name: string; from?: string }) => {
  const peer = findPeer(usePeers(), name, from);
  const status = STATUS[peer?.status ?? ''] ?? null;
  const title = peer?.title ? cleanConversationTitle(peer.title) : null;
  const hint = [title, peer?.name ?? name, status?.label ?? (peer ? null : t('agents.peerChip.closed'))]
    .filter(Boolean)
    .join(' · ');
  const content = (
    <>
      <ProviderIcon provider="claude" size="sm" />
      <span className="tw-peer-chip-title">{title ?? peer?.name ?? name}</span>
      {title && <small>{peer?.name}</small>}
      {status && <span className={cn('tw-row-dot', `tw-row-dot-${status.dot}`)} aria-label={status.label} />}
    </>
  );
  if (!peer?.tabId)
    return (
      <span className="tw-peer-chip tw-peer-chip-gone" title={hint}>
        {content}
      </span>
    );
  return (
    <button
      type="button"
      className="tw-peer-chip"
      title={t('agents.peerChip.open', { hint })}
      onClick={() => window.dispatchEvent(new CustomEvent('hl:open-conversation', { detail: peer.tabId }))}
    >
      {content}
    </button>
  );
};

export default PeerChip;
