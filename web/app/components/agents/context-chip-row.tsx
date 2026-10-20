import { File as FileIcon, GitPullRequest, MessageSquare, Ticket, X } from 'lucide-react';
import { TwChip } from '@/components/agents/ui';
import type { TContextChip } from '@/lib/hl-context-chips';
import { t } from '@/lib/i18n';

const CHIP_ICON: Record<TContextChip['kind'], typeof MessageSquare> = {
  conversation: MessageSquare,
  task: Ticket,
  mr: GitPullRequest,
  file: FileIcon,
};

interface IContextChipRowProps {
  chips: TContextChip[];
  onRemove: (id: string) => void;
}

/** Attached «@»-context chips, above the editor: removable by click or Enter/Space once focused (a plain button). */
const ContextChipRow = ({ chips, onRemove }: IContextChipRowProps) => {
  if (!chips.length) return null;
  return (
    <div className="tw-at-chips" role="list" aria-label={t('agents.contextChipRow.listLabel')}>
      {chips.map((chip) => {
        const Icon = CHIP_ICON[chip.kind];
        return (
          <TwChip
            key={chip.id}
            role="listitem"
            icon={<Icon size={12} />}
            onClick={() => onRemove(chip.id)}
            title={t('agents.contextChipRow.remove')}
            aria-label={t('agents.contextChipRow.removeLabel', { label: chip.label })}
          >
            {chip.label}
            <X size={11} aria-hidden="true" />
          </TwChip>
        );
      })}
    </div>
  );
};

export default ContextChipRow;
