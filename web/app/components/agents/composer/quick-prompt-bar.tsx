import { useRef, type KeyboardEvent } from 'react';
import type { IQuickPrompt } from '@/lib/hl-quick-prompts';
import { t } from '@/lib/i18n';

interface IQuickPromptBarProps {
  prompts: readonly IQuickPrompt[];
  /** The prompt's text goes into the composer; nothing is sent. */
  onSelect: (prompt: string) => void;
}

/** The person's saved prompts as a row of chips under the composer; the arrow keys walk along it. */
const QuickPromptBar = ({ prompts, onSelect }: IQuickPromptBarProps) => {
  const bar = useRef<HTMLDivElement>(null);
  if (prompts.length === 0) return null;

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
    const chips = [...(bar.current?.querySelectorAll<HTMLButtonElement>('[data-quick-prompt]') ?? [])];
    if (!chips.length) return;
    event.preventDefault();
    const at = chips.findIndex((chip) => chip === document.activeElement);
    const step = event.key === 'ArrowRight' ? 1 : -1;
    chips[(at + step + chips.length) % chips.length].focus();
  };

  return (
    <div
      ref={bar}
      role="toolbar"
      className="tw-quick-prompts"
      aria-label={t('workspace.quickPrompts.label')}
      onKeyDown={onKeyDown}
    >
      {prompts.map((prompt) => (
        <button
          key={prompt.id}
          type="button"
          className="tw-quick-prompt"
          data-quick-prompt={prompt.id}
          title={prompt.prompt}
          onClick={() => onSelect(prompt.prompt)}
        >
          {prompt.name}
        </button>
      ))}
    </div>
  );
};

export default QuickPromptBar;
