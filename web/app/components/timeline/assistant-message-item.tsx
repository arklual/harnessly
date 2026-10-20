// One reply of the agent: its markdown, and under the last reply of a turn the copy button and the model's name.
import { useState, type ReactNode } from 'react';
import { Check, Copy } from 'lucide-react';
import CachedMarkdown from '@/components/timeline/cached-markdown';
import { MESSAGE_COMPONENTS, MESSAGE_PLUGINS, PROSE } from '@/components/timeline/markdown-parts';
import { copyToClipboard } from '@/lib/clipboard';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

interface IAssistantMessageItemProps {
  entry: { id: string; markdown?: string; type?: string; timestamp?: number; model?: string };
  provider?: string;
  muted?: boolean;
  showActions?: boolean;
  /** The model that wrote it, shown next to the actions. */
  label?: string;
  emptyText?: string;
  actions?: ReactNode;
}

export default function AssistantMessageItem({
  entry,
  provider = 'claude',
  muted,
  showActions = true,
  label,
  emptyText,
  actions,
}: IAssistantMessageItemProps) {
  const [copied, setCopied] = useState(false);
  const text = entry.markdown ?? '';
  const copy = async () => {
    if (await copyToClipboard(text)) {
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    }
  };
  return (
    <div className="agent-message" data-provider={provider} data-muted={muted || undefined}>
      {text.trim() ? (
        <div className={cn('agent-message-body leading-7', PROSE)}>
          <CachedMarkdown cacheKey="message" remarkPlugins={MESSAGE_PLUGINS} components={MESSAGE_COMPONENTS}>
            {text}
          </CachedMarkdown>
        </div>
      ) : emptyText ? (
        <p className="text-xs text-muted-foreground">{emptyText}</p>
      ) : null}
      {showActions && text.trim() && (
        <div className="agent-message-actions">
          <button
            type="button"
            onClick={() => void copy()}
            aria-label={t('timeline.assistantMessageItem.copyAnswer')}
          >
            {copied ? <Check size={13} /> : <Copy size={13} />}
            <span>
              {copied ? t('timeline.assistantMessageItem.copied') : t('timeline.assistantMessageItem.copy')}
            </span>
          </button>
          {actions}
          {label && <span className="agent-message-model">{label}</span>}
        </div>
      )}
    </div>
  );
}
