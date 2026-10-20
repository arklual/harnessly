import { Check, Eye, Hand, X, type LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { TProviderStatus } from '@/components/agents/provider-icon';

interface IStatusOrbProps {
  status: TProviderStatus;
  label?: string;
  className?: string;
}

/** Claude Desktop's session tile glyphs; `gone` is the same cross without the red tile. */
const GLYPHS: Partial<Record<TProviderStatus, LucideIcon>> = {
  'needs-input': Hand,
  ready: Eye,
  done: Check,
  failed: X,
  gone: X,
};

/** Conversation state. Background work has no orbit any more: it makes the conversation working instead. */
const StatusOrb = ({ status, label, className }: IStatusOrbProps) => {
  const Glyph = GLYPHS[status];
  return (
    <span className={cn('tw-orb', `tw-orb-${status}`, className)} role="img" aria-label={label} title={label}>
      {Glyph ? (
        <span className="tw-orb-tile">
          <Glyph aria-hidden="true" strokeWidth={2.6} />
        </span>
      ) : (
        <span className="tw-orb-core" />
      )}
    </span>
  );
};

export default StatusOrb;
