import { isValidElement, useRef, useState, type HTMLAttributes, type ReactElement } from 'react';
import { Check, Copy, Code2 } from 'lucide-react';
import { toast } from 'sonner';
import { t } from '@/lib/i18n';

export default function MessageCodeBlock({ children }: HTMLAttributes<HTMLPreElement>) {
  const ref = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(ref.current?.textContent ?? '');
      setCopied(true);
      setTimeout(() => setCopied(false), 1800);
    } catch {
      toast.error(t('timeline.messageCodeBlock.copyFailed'));
    }
  }
  const language = isValidElement(children)
    ? String((children as ReactElement<{ className?: string }>).props.className ?? '').match(
        /language-([\w+-]+)/,
      )?.[1]
    : undefined;
  return (
    <div className="message-code">
      <div className="message-code-toolbar">
        <span>
          <Code2 size={13} />
          {language ?? t('timeline.messageCodeBlock.code')}
        </span>
        <button
          type="button"
          onClick={copy}
          aria-label={
            copied ? t('timeline.messageCodeBlock.codeCopied') : t('timeline.messageCodeBlock.copyCode')
          }
        >
          {copied ? <Check size={13} /> : <Copy size={13} />}
          {copied ? t('timeline.messageCodeBlock.copied') : t('timeline.messageCodeBlock.copy')}
        </button>
      </div>
      <pre ref={ref}>{children}</pre>
    </div>
  );
}
