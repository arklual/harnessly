import { useEffect, useRef, useState, type KeyboardEvent, type RefObject } from 'react';
import { toast } from 'sonner';
import SlashCommandPopover from '@/components/agents/slash-command-popover';
import type { PromptEditorHandle } from '@/components/workspace/prompt-editor';
import useSlashCommands from '@/hooks/use-slash-commands';
import { OPEN_LAUNCH_PICKER_EVENT, type IOpenLaunchPickerDetail } from '@/lib/hl-launch-options';
import type { ICommandItem } from '@/lib/hl-command-catalog/types';
import type { TNewConversationKind } from '@/lib/hl-new-conversation';
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

/** Handles a key in the welcome editor before the composer does; returns true when it consumed the key. */
export type TWelcomeKeyHandler = (event: KeyboardEvent<HTMLDivElement>) => boolean;

export interface IWelcomeSlashSlotProps {
  kind: TNewConversationKind;
  machineId?: string;
  value: string;
  onValueChange: (value: string) => void;
  editorRef: RefObject<PromptEditorHandle | null>;
  /** The slot installs its key handler here (menu navigation, Tab to complete). */
  keyHandlerRef: RefObject<TWelcomeKeyHandler | null>;
  /** Starts the conversation with this line as its first command. */
  onRunCommand: (line: string) => void;
}

/** The chat has no slash commands. */
const COMMAND_PROVIDER = {
  claude: 'claude',
  codex: 'codex',
  chat: null,
} as const satisfies Record<TNewConversationKind, TAgentProvider | null>;

const LINE_RE = /^\/(\S+)(?:\s+[\s\S]*)?$/;

/**
 * The slash-command list of the welcome composer: only commands that may start a
 * conversation are offered; `/model` and `/effort` open the composer's chip; Enter on a command line starts
 * the conversation with it as the first command, sent by the conversation's composer once the agent is ready.
 */
const WelcomeSlashSlot = ({
  kind,
  machineId,
  value,
  onValueChange,
  editorRef,
  keyHandlerRef,
  onRunCommand,
}: IWelcomeSlashSlotProps) => {
  const provider = COMMAND_PROVIDER[kind];
  const [focused, setFocused] = useState(false);
  const anchorRef = useRef<HTMLSpanElement>(null);

  const openChip = (section: 'model' | 'effort') => {
    const detail: IOpenLaunchPickerDetail = { section };
    window.dispatchEvent(new CustomEvent(OPEN_LAUNCH_PICKER_EVENT, { detail }));
  };

  const runItem = (item: ICommandItem | null, line: string) => {
    if (item?.execution === 'model' || item?.execution === 'effort') {
      if (!line.trim().includes(' ')) {
        onValueChange('');
        openChip(item.execution);
        return;
      }
      toast.error(t('agents.welcomeSlashSlot.useChip'));
      return;
    }
    if (item && !item.welcome) {
      toast.error(t('agents.welcomeSlashSlot.startedOnly'));
      return;
    }
    onRunCommand(line);
  };

  const slash = useSlashCommands({
    provider,
    machineId,
    cwd: null,
    value,
    setValue: (next) => {
      onValueChange(next);
      editorRef.current?.focusEnd();
    },
    active: focused,
    welcome: true,
    onRun: (item, line) => runItem(item, line),
  });

  // The editor belongs to the welcome composer; the combobox wiring is applied to it from here.
  const editorElement = () =>
    anchorRef.current?.closest('.tw-welcome-composer')?.querySelector<HTMLElement>('[role="textbox"]') ??
    null;

  useEffect(() => {
    const editor = editorElement();
    if (!editor) return;
    const onFocus = () => setFocused(true);
    if (document.activeElement === editor) onFocus();
    editor.addEventListener('focus', onFocus);
    return () => editor.removeEventListener('focus', onFocus);
  }, []);

  useEffect(() => {
    const editor = editorElement();
    if (!editor) return;
    const attributes = {
      'aria-autocomplete': provider ? 'list' : null,
      'aria-expanded': provider ? String(slash.open) : null,
      'aria-controls': slash.open ? slash.listId : null,
      'aria-activedescendant': slash.activeDescendant ?? null,
    };
    for (const [name, attribute] of Object.entries(attributes)) {
      if (attribute === null) editor.removeAttribute(name);
      else editor.setAttribute(name, attribute);
    }
  }, [provider, slash.activeDescendant, slash.listId, slash.open]);

  useEffect(() => {
    keyHandlerRef.current = (event) => {
      if (slash.handleKeyDown(event)) return true;
      if (!provider || event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return false;
      const line = value.trim();
      const match = line.match(LINE_RE);
      if (!match || line.includes('\n')) return false;
      event.preventDefault();
      runItem(slash.resolve(match[1]), line);
      return true;
    };
    return () => {
      keyHandlerRef.current = null;
    };
  });

  return (
    <>
      <span ref={anchorRef} hidden />
      {provider && <SlashCommandPopover slash={slash} provider={provider} welcome />}
      {provider && slash.argumentHint && !slash.open && (
        <p className="tw-slash-hint" aria-live="polite">
          {slash.argumentHint}
        </p>
      )}
    </>
  );
};

export default WelcomeSlashSlot;
