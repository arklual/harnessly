// The prompt box under an agent conversation. Enter sends, Shift+Enter breaks the line, the round button sends or,
// while the agent works and the field is empty, stops it. A message typed while the agent works joins its queue.
// It carries the page's composer parts: slash commands, context chips, picked elements and diff comments,
// attachments (uploaded to the machine, their paths go into the prompt), dictation and the session's chips.
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type DragEvent,
  type KeyboardEvent,
} from 'react';
import { Paperclip, Plus } from 'lucide-react';
import { toast } from 'sonner';
import PromptEditor, { type PromptEditorHandle } from '@/components/workspace/prompt-editor';
import ComposerSendButton, { SEND_KIND } from '@/components/agents/composer/composer-send-button';
import AttachmentChip, { type IAttachmentChip } from '@/components/agents/composer/attachment-chip';
import PromptQueueStack from '@/components/agents/composer/prompt-queue-stack';
import { ComposerSessionModel, ComposerSessionStatus } from '@/components/agents/composer-session-meta';
import SlashCommandPopover from '@/components/agents/slash-command-popover';
import TuiMenuMirror from '@/components/agents/tui-menu-mirror';
import ContextChipRow from '@/components/agents/context-chip-row';
import ContextPickerPopover from '@/components/agents/context-picker-popover';
import AnnotationChipRow from '@/components/agents/annotation-chip-row';
import DiffCommentChipRow from '@/components/agents/diff-comment-chip-row';
import VoiceMicButton, { useVoiceMenu } from '@/components/agents/chat/voice-mic-button';
import VoiceRecorderBar, { VoiceLiveDraft } from '@/components/agents/chat/voice-recorder-bar';
import AttachmentViewer, { type IAttachmentView } from '@/components/agents/inspector/attachment-viewer';
import {
  CONFIRM_TONE,
  TW_MENU_KIND,
  TwConfirm,
  TwMenuButton,
  type TTwMenuEntry,
} from '@/components/agents/ui';
import useSlashCommands, { useSlashExecutor } from '@/hooks/use-slash-commands';
import useContextPicker from '@/hooks/use-context-picker';
import useVoiceComposer from '@/hooks/use-voice-composer';
import useConfigStore from '@/hooks/use-config-store';
import { enqueuePrompt } from '@/hooks/use-prompt-queue';
import { clearInputDraft, loadInputDraft, saveInputDraft } from '@/hooks/use-web-input';
import { dictationKeyAction } from '@/lib/dictation-intent';
import { appendContextBlock } from '@/lib/hl-context-chips';
import { appendAnnotations } from '@/lib/hl-annotation-format';
import { commentPlaceholder, composeAnnotationComment } from '@/lib/hl-annotation-composer';
import { removeAnnotation, useAnnotations, type TComposerAnnotation } from '@/lib/hl-annotations-store';
import { appendDiffComments, type IDiffComment } from '@/lib/hl-diff-comment';
import { removeDiffComment, useDiffComments } from '@/lib/hl-diff-comments-store';
import { attachmentKindOf } from '@/lib/attachment-media';
import { appFetch } from '@/lib/app-path';
import { appConfig } from '@/platform/config';
import { agents } from '@/runtime/endpoints';
import { t } from '@/lib/i18n';

const MAX_ATTACHMENTS = 20;

interface IAttachment extends IAttachmentChip {
  /** The file's path on the machine once uploaded. */
  path: string;
}

const newId = () =>
  crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`;

/** A path the agent reads as one word: spaces and quotes escaped the way a shell would. */
const promptPath = (path: string) => path.replace(/([\s'"\\])/g, '\\$1');

/** Uploads one file to the machine (the runtime's /api/upload-image or /api/upload-file) and answers its path. */
const upload = async (file: File, workspaceId: string | undefined, tabId: string): Promise<string> => {
  const prefix = appConfig().uploadHeaderPrefix;
  const image = attachmentKindOf(file.type, file.name) === 'image';
  const headers: Record<string, string> = {
    'content-type': file.type || 'application/octet-stream',
    [`${prefix}filename`]: encodeURIComponent(file.name),
    [`${prefix}tab-id`]: tabId,
  };
  if (workspaceId) headers[`${prefix}ws-id`] = workspaceId;
  const response = await appFetch(image ? '/api/upload-image' : '/api/upload-file', {
    method: 'POST',
    headers,
    body: file,
  });
  const body = (await response.json().catch(() => null)) as { path?: string; error?: string } | null;
  if (!response.ok || !body?.path) throw new Error(body?.error ?? `HTTP ${response.status}`);
  return body.path;
};

interface IAgentComposerProps {
  tabId: string;
  sessionName: string;
  workspaceId?: string;
  provider: 'claude' | 'codex';
  working: boolean;
  /** The agent is not running: nothing may be typed into the bare shell of its tab. */
  stopped?: boolean;
  /** Folder the agent works in: project commands and files are looked up from it. */
  cwd: string | null;
  machineId?: string;
  /** The text of a message that went out now (not into the queue), for the timeline to show at once. */
  onSent?: (text: string) => void;
  onOpenTerminal?: () => void;
}

export default function AgentComposer({
  tabId,
  sessionName,
  workspaceId,
  provider,
  working,
  stopped = false,
  cwd,
  machineId,
  onSent,
  onOpenTerminal,
}: IAgentComposerProps) {
  const [value, setValue] = useState(() => loadInputDraft(tabId));
  const [sending, setSending] = useState(false);
  const [focusedOnce, setFocusedOnce] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [attachments, setAttachments] = useState<IAttachment[]>([]);
  const [viewing, setViewing] = useState<IAttachmentView | null>(null);
  const editor = useRef<PromptEditorHandle | null>(null);
  const fileInput = useRef<HTMLInputElement | null>(null);
  const uploads = useRef(new Map<string, Promise<string>>());
  const annotations = useAnnotations(tabId);
  const diffComments = useDiffComments(tabId);
  const voiceAutoSend = useConfigStore((s) => s.voiceSettings.autoSend);

  useEffect(() => saveInputDraft(tabId, value), [tabId, value]);
  useEffect(
    () => () => {
      setAttachments((list) => {
        list.forEach((a) => URL.revokeObjectURL(a.thumbnail));
        return [];
      });
    },
    [],
  );

  const setValueAndFocus = useCallback((next: string) => {
    setValue(next);
    editor.current?.focusEnd();
  }, []);
  const openTerminal = useCallback(() => onOpenTerminal?.(), [onOpenTerminal]);

  const slashLine = useRef<(line: string) => Promise<boolean>>(async () => false);
  const slash = useSlashCommands({
    provider,
    machineId,
    cwd,
    value,
    setValue: setValueAndFocus,
    active: focusedOnce,
    onRun: (_item, line) => {
      setValue(line);
      void slashLine.current(line).then((sent) => {
        if (!sent) return;
        setValue('');
        clearInputDraft(tabId);
      });
    },
  });
  const executor = useSlashExecutor({
    provider,
    sessionName,
    resolve: slash.resolve,
    setValue: setValueAndFocus,
    openAll: slash.openAll,
    onOpenTerminal: openTerminal,
  });
  useEffect(() => {
    slashLine.current = executor.execute;
  }, [executor.execute]);
  const contextPicker = useContextPicker({ value, setValue: setValueAndFocus, cwd });

  const addFiles = useCallback(
    (files: File[]) => {
      const room = MAX_ATTACHMENTS - attachments.length;
      if (room <= 0) {
        toast.error(t('workspace.agentComposer.tooManyAttachments', { max: MAX_ATTACHMENTS }));
        return;
      }
      const added = files.slice(0, room).map((file): IAttachment => {
        const id = newId();
        const kind = attachmentKindOf(file.type, file.name);
        const job = upload(file, workspaceId, tabId);
        uploads.current.set(id, job);
        job.then(
          (path) =>
            setAttachments((list) => list.map((a) => (a.id === id ? { ...a, path, uploading: false } : a))),
          (error: unknown) => {
            setAttachments((list) => list.filter((a) => a.id !== id));
            toast.error(t('workspace.agentComposer.uploadFailed', { name: file.name }), {
              description: error instanceof Error ? error.message : undefined,
            });
          },
        );
        return {
          id,
          filename: file.name,
          thumbnail: URL.createObjectURL(file),
          kind,
          uploading: true,
          path: '',
        };
      });
      setAttachments((list) => [...list, ...added]);
    },
    [attachments.length, tabId, workspaceId],
  );

  const removeAttachment = (id: string) =>
    setAttachments((list) => {
      const gone = list.find((a) => a.id === id);
      if (gone) URL.revokeObjectURL(gone.thumbnail);
      uploads.current.delete(id);
      return list.filter((a) => a.id !== id);
    });

  const forgetSent = (sent: readonly TComposerAnnotation[], sentDiff: readonly IDiffComment[]) => {
    for (const annotation of sent) removeAnnotation(tabId, annotation.id);
    for (const comment of sentDiff) removeDiffComment(tabId, comment.id);
  };

  const submit = async (override?: string) => {
    const typed = override ?? value;
    const trimmed = typed.trim();
    if (sending || executor.running || stopped) return;
    if (/^\/\S/.test(trimmed) && !trimmed.includes('\n') && attachments.length === 0) {
      const sent = await executor.execute(trimmed);
      if (sent) {
        setValue('');
        clearInputDraft(tabId);
      }
      return;
    }
    const composed = composeAnnotationComment(typed, annotations);
    if (
      !trimmed &&
      attachments.length === 0 &&
      composed.annotations.length === 0 &&
      diffComments.length === 0
    )
      return;
    setSending(true);
    try {
      const paths = await Promise.all(
        attachments.map((a) =>
          a.uploading
            ? (uploads.current.get(a.id) ?? Promise.reject(new Error(a.filename)))
            : Promise.resolve(a.path),
        ),
      );
      let text = appendAnnotations(
        appendDiffComments(appendContextBlock(composed.text, contextPicker.chips), diffComments),
        composed.annotations,
      );
      if (paths.length) text = [text.trim(), paths.map(promptPath).join(' ')].filter(Boolean).join('\n\n');
      if (working) {
        await enqueuePrompt(
          machineId,
          tabId,
          text,
          attachments.filter((a) => a.kind === 'image').map((a) => a.thumbnail),
          async () => ({ text, images: [] }),
        );
      } else {
        onSent?.(text);
        await agents.send(tabId, text, newId());
      }
      forgetSent(composed.annotations, diffComments);
      if (contextPicker.chips.length) contextPicker.clearChips();
      setAttachments([]);
      setValue('');
      clearInputDraft(tabId);
    } catch (failure) {
      toast.error(t('workspace.agentComposer.sendFailed'), {
        description: failure instanceof Error ? failure.message : undefined,
      });
    } finally {
      setSending(false);
      editor.current?.focus();
    }
  };

  const dictation = useVoiceComposer({
    value,
    onValueChange: setValue,
    onAutoSend: (text) => void submit(text),
    disabled: sending,
  });

  const voiceMenu = useVoiceMenu();
  const uploading = attachments.some((a) => a.uploading);
  const plusItems: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.action,
      id: 'files',
      label: t('workspace.agentComposer.filesAndImages'),
      icon: <Paperclip />,
      onSelect: () => fileInput.current?.click(),
    },
    { kind: TW_MENU_KIND.separator, id: 'sep-voice' },
    ...voiceMenu.entries,
  ];

  const stop = () =>
    void agents.interrupt(tabId).catch(() => toast.error(t('workspace.agentComposer.stopFailed')));

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (slash.handleKeyDown(event)) return;
    if (contextPicker.handleKeyDown(event)) return;
    if (!event.nativeEvent.isComposing) {
      const action = dictationKeyAction(dictation.phase, event, false);
      if (action === 'finish-and-send') {
        event.preventDefault();
        dictation.finishAndSend();
        return;
      }
      if (action === 'finish') {
        event.preventDefault();
        dictation.finish();
        return;
      }
    }
    if (event.key === 'Escape' && working && !value.trim()) {
      event.preventDefault();
      stop();
      return;
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void submit();
    }
  };

  const onDrop = (event: DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    setDragging(false);
    const files = [...event.dataTransfer.files];
    if (files.length) addFiles(files);
  };
  const onPick = (event: ChangeEvent<HTMLInputElement>) => {
    const files = [...(event.target.files ?? [])];
    event.target.value = '';
    if (files.length) addFiles(files);
  };

  const hasContent =
    value.trim().length > 0 || attachments.length > 0 || annotations.length > 0 || diffComments.length > 0;
  const showStop = working && !hasContent;
  const placeholder = stopped
    ? t('workspace.agentComposer.stoppedPlaceholder')
    : (commentPlaceholder(annotations) ??
      (working ? t('workspace.agentComposer.queuedPlaceholder') : t('workspace.agentComposer.placeholder')));

  return (
    <div className="tw-agent-composer relative" data-testid="composer">
      <SlashCommandPopover slash={slash} provider={provider} />
      <ContextPickerPopover picker={contextPicker} />
      <PromptQueueStack tabId={tabId} onEdit={(item) => setValueAndFocus(item.text)} />
      <div
        className="agent-composer z-10"
        data-dragging={dragging || undefined}
        data-recording={dictation.phase ? '' : undefined}
        onDragOver={(event) => {
          if (event.dataTransfer.types.includes('Files')) {
            event.preventDefault();
            setDragging(true);
          }
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={onDrop}
        onFocus={() => setFocusedOnce(true)}
      >
        <ContextChipRow chips={contextPicker.chips} onRemove={contextPicker.removeChip} />
        <AnnotationChipRow tabId={tabId} />
        <DiffCommentChipRow tabId={tabId} />
        {attachments.length > 0 && (
          <div className="tw-cattach">
            {attachments.map((a) => (
              <AttachmentChip
                key={a.id}
                attachment={a}
                removeLabel={t('workspace.agentComposer.removeAttachment', { name: a.filename })}
                onOpen={setViewing}
                onRemove={removeAttachment}
              />
            ))}
          </div>
        )}
        <PromptEditor
          data-testid="composer-input"
          editorRef={editor}
          value={value}
          onValueChange={setValue}
          onKeyDown={onKeyDown}
          onFiles={addFiles}
          placeholder={placeholder}
          aria-label={t('workspace.agentComposer.messageLabel')}
          aria-expanded={slash.open || contextPicker.open}
          aria-controls={slash.open ? slash.listId : contextPicker.open ? contextPicker.listId : undefined}
          aria-activedescendant={slash.open ? slash.activeDescendant : contextPicker.activeDescendant}
          className={`agent-prompt-editor tw-ui-editor-rows tw-composer-editor${dictation.preview ? ' tw-editor-behind-draft' : ''}`}
        />
        {dictation.preview && (
          <VoiceLiveDraft
            value={value}
            preview={dictation.preview}
            className="agent-prompt-editor tw-ui-editor-rows tw-composer-editor"
          />
        )}
        {slash.argumentHint && !slash.open && (
          <p className="tw-slash-hint" aria-live="polite">
            {slash.argumentHint}
          </p>
        )}
        <input ref={fileInput} type="file" multiple hidden onChange={onPick} />
        {dictation.phase ? (
          <VoiceRecorderBar dictation={dictation} sendsOnFinish={voiceAutoSend} />
        ) : (
          <div className="tw-cbar">
            <div className="tw-cbar-start">
              {voiceMenu.dialog}
              <span className="tw-cplus-anchor" onPointerDown={voiceMenu.refresh}>
                <TwMenuButton
                  items={plusItems}
                  menuLabel={t('workspace.agentComposer.addMenu')}
                  side="top"
                  align="start"
                  className="tw-cbtn tw-cplus"
                  data-busy={uploading || undefined}
                  aria-label={
                    uploading ? t('workspace.agentComposer.uploading') : t('workspace.agentComposer.addLabel')
                  }
                  title={
                    uploading ? t('workspace.agentComposer.uploading') : t('workspace.agentComposer.addTitle')
                  }
                >
                  <Plus aria-hidden="true" />
                </TwMenuButton>
              </span>
              <ComposerSessionStatus tabId={tabId} sessionName={sessionName} provider={provider} />
            </div>
            <div className="tw-cbar-end">
              <ComposerSessionModel tabId={tabId} sessionName={sessionName} provider={provider} />
              <VoiceMicButton dictation={dictation} disabled={sending} withSettings={false} />
              <ComposerSendButton
                testId="composer-send"
                kind={showStop ? SEND_KIND.stop : SEND_KIND.send}
                disabled={!showStop && (!hasContent || stopped)}
                busy={sending}
                codex={provider === 'codex'}
                onClick={showStop ? stop : () => void submit()}
              />
            </div>
          </div>
        )}
      </div>
      {executor.mirror && (
        <TuiMenuMirror
          mirror={executor.mirror}
          sessionName={sessionName}
          provider={provider}
          onClose={executor.closeMirror}
          onOpenTerminal={openTerminal}
        />
      )}
      {executor.confirm && (
        <TwConfirm
          open
          title={t('workspace.agentComposer.runCommand', { name: executor.confirm.item.name })}
          description={executor.confirm.item.note ?? undefined}
          confirmLabel={
            executor.confirm.item.danger
              ? t('workspace.agentComposer.deleteForever')
              : t('workspace.agentComposer.run')
          }
          tone={executor.confirm.item.danger ? CONFIRM_TONE.danger : CONFIRM_TONE.default}
          pending={executor.confirmPending}
          error={executor.confirmError}
          onConfirm={executor.acceptConfirm}
          onCancel={executor.cancelConfirm}
        />
      )}
      {viewing && <AttachmentViewer item={viewing} onClose={() => setViewing(null)} />}
    </div>
  );
}
