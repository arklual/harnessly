import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import PromptEditor, { type PromptEditorHandle } from '@/components/workspace/prompt-editor';
import ComposerSendButton from '@/components/agents/composer/composer-send-button';
import PromptQueueStack from '@/components/agents/composer/prompt-queue-stack';
import { enqueuePrompt } from '@/hooks/use-prompt-queue';
import { loadInputDraft, saveInputDraft } from '@/hooks/use-web-input';
import { UNASSIGNED_TAB } from '@/lib/hl-pending-launch';
import { t } from '@/lib/i18n';

// The tab's composer takes over the focus in the commit that removes this one: a letter typed in between lands in it.
let focusHandover = false;
export const takeLaunchComposerFocus = (): boolean => {
  const handover = focusHandover;
  focusHandover = false;
  return handover;
};

interface ILaunchComposerProps {
  /** The conversation's tab, or `UNASSIGNED_TAB` while the machine is still creating it. */
  tabId: string;
  machineId: string | undefined;
  provider: 'claude' | 'codex';
}

/**
 * The composer of a conversation that is being created, in its place from the press: what is typed here is
 * the tab's draft, so the real composer picks it up when the tab arrives, and a message sent here goes to the
 * machine's queue, which hands it to Claude once it has started. Before the tab exists, a sent message waits here.
 */
const LaunchComposer = ({ tabId, machineId, provider }: ILaunchComposerProps) => {
  const editorRef = useRef<PromptEditorHandle>(null);
  const [value, setValue] = useState(() => loadInputDraft(tabId));
  const [held, setHeld] = useState<string[]>([]);
  const assigned = tabId !== UNASSIGNED_TAB;
  // Codex keeps its own prompt handling: its first extra message stays a draft for its composer.
  const queues = provider === 'claude';

  // Saved with every keystroke, not after the render: the composer of the tab replaces this one within a frame and
  // loads the draft from there, so a letter typed in that frame must already be stored.
  const change = (next: string) => {
    setValue(next);
    saveInputDraft(tabId, next);
  };
  // The draft moves to the tab the moment it exists.
  useEffect(() => {
    if (tabId === UNASSIGNED_TAB) return;
    saveInputDraft(tabId, value);
    saveInputDraft(UNASSIGNED_TAB, '');
  }, [tabId, value]);

  const rootRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    editorRef.current?.focus();
  }, []);
  useLayoutEffect(() => {
    const root = rootRef.current;
    return () => {
      focusHandover = !!root && root.contains(document.activeElement);
    };
  }, []);

  useEffect(() => {
    if (!assigned || held.length === 0) return;
    const sending = held;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- hands the held messages over once the tab exists
    setHeld([]);
    for (const text of sending)
      void enqueuePrompt(machineId, tabId, text, [], async () => ({ text, images: [] })).catch(() =>
        setValue((current) => (current.trim() ? `${current}\n${text}` : text)),
      );
  }, [assigned, held, machineId, tabId]);

  const send = () => {
    const text = value.trim();
    if (!text || !queues) return;
    change('');
    if (assigned)
      void enqueuePrompt(machineId, tabId, text, [], async () => ({ text, images: [] })).catch(() =>
        setValue(text),
      );
    else setHeld((current) => [...current, text]);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      send();
    }
  };

  return (
    <div ref={rootRef} className="mx-auto w-full max-w-content px-3" data-launch-composer>
      {assigned && queues && (
        <PromptQueueStack
          tabId={tabId}
          onEdit={(item) => setValue((current) => (current.trim() ? `${current}\n${item.text}` : item.text))}
          launch="starting"
        />
      )}
      {held.length > 0 && (
        <ul className="tw-pqueue" aria-label={t('composer.launchComposer.queue')}>
          <li className="tw-pqueue-note" role="status">
            {t('composer.launchComposer.creating')}
          </li>
          {held.map((text, index) => (
            <li key={index} className="tw-pqueue-row" aria-busy>
              <span className="tw-pqueue-text" title={text}>
                {text}
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="agent-composer z-10">
        <PromptEditor
          editorRef={editorRef}
          value={value}
          onValueChange={change}
          onKeyDown={onKeyDown}
          placeholder={t('composer.launchComposer.placeholder')}
          aria-label={t('composer.launchComposer.input')}
          className="agent-prompt-editor tw-ui-editor-rows tw-composer-editor w-full bg-transparent"
        />
        <div className="tw-cbar">
          <div className="tw-cbar-start" />
          <div className="tw-cbar-end">
            <ComposerSendButton
              disabled={!value.trim() || !queues}
              codex={provider === 'codex'}
              label={t('composer.launchComposer.send')}
              onClick={send}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

export default LaunchComposer;
