// The start page's field: the first message creates a conversation on the active machine and opens it (spec §3,
// conversation.create with `first`). The field is busy-free and the words stay in it until the machine has created
// the chat; if it refuses, they are still there and the reason is on screen.
import { uploadSrc } from '@/next/commands/uploads';
import { AttachmentSrc } from '@/next/ui/attachment-thumb';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { uuidv7 } from '@/next/core/ids';
import { useBeta } from '@/next/stores/beta';
import type { IModelChoice } from '@/next/features/composer/agents';
import { choiceKey, launchPrompt, toggleChoice } from '@/next/features/composer/multi-model';
import { createConversation, type NewConversation } from '@/next/commands';
import { failureText } from '@/next/bindings/outcome';
import { useAgentLimits } from '@/next/bindings/use-limits';
import { limitsProviderOf } from '@/next/features/limits/model';
import { useUploads } from '@/next/bindings/use-uploads';
import { rememberChoice } from '@/next/features/composer/start-choice';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { useSlashSource } from '@/next/bindings/slash-source';
import { UI_REQUEST, requestUi } from '@/next/stores/ui-requests';
import type { ISlashActions } from '@/next/features/composer/slash-model';
import Composer from '@/next/features/composer/composer';
import type { IModelPickerProps } from '@/next/features/composer/model-picker';
import type { Conversation, ConvId, MachineId } from '@/next/protocol/v2';
import { clearDraft, NEW_CHAT } from '@/next/stores/drafts';
import { useDraftField } from '@/next/bindings/use-draft';
import { useFieldStash } from '@/next/bindings/use-stash';
import {
  clearMentions,
  MENTION_KIND,
  removeMention,
  useMentions,
  withMentions,
} from '@/next/stores/mentions';
import { limitsOf } from '@/next/stores/machines';
import { useVoice } from '@/next/bindings/use-voice';
import { useTakesMenu } from '@/next/bindings/use-takes';
import DockBar from '@/next/features/composer/dock-bar';
import { useEnterSends } from '@/next/stores/me';
import { useDefaultCwd } from '@/next/stores/projects';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

/** The folder a new conversation starts in: where the machine's latest one works, else the machine's default one. */
export const startFolder = (
  convs: readonly Conversation[] | null,
  machine: MachineId,
  defaultCwd: string | null = null,
): string | null => {
  const latest = (convs ?? [])
    .filter((c) => c.machineId === machine)
    .sort((a, b) => b.createdAt - a.createdAt)[0];
  return latest?.cwd ?? defaultCwd;
};

interface IHomeComposerProps {
  machine: MachineId | null;
  conversations: readonly Conversation[] | null;
  /** The project the chat starts in (picked in the sidebar); without one it starts in the usual folder. */
  projectId?: string | null;
  /** The project is the person's (the cluster keeps the sidebar): this gives its project on the machine the chat
   *  starts on, made there when it has none. Without it the project is the machine's own. */
  resolveProject?: (projectId: string, machine: MachineId) => Promise<string>;
  onCreated: (id: ConvId) => void;
  /** The machine the chat starts on, picked in the model's popover; its agents are this machine's. */
  machines?: IModelPickerProps['machine'];
}

const noChat = () => undefined;

const HomeComposer = ({
  machine,
  conversations,
  projectId = null,
  resolveProject,
  onCreated,
  machines = null,
}: IHomeComposerProps) => {
  const enterSends = useEnterSends();
  // Arriving with something taken from the Apps screen, the person goes on in the field.
  const field = useRef<HTMLTextAreaElement>(null);
  const { text, files, setText, read } = useDraftField(NEW_CHAT, field);
  const mentions = useMentions(NEW_CHAT);
  const stash = useFieldStash(NEW_CHAT);
  const uploads = useUploads(machine, NEW_CHAT, files);
  const { notice: voiceNotice, insert, ...voice } = useVoice(machine, text, setText, read);
  const takes = useTakesMenu(insert);
  const [creating, setCreating] = useState(false);
  // Enter pressed before the machine's list arrives waits for it and then sends, instead of being lost.
  const waiting = useRef<(() => void) | null>(null);
  const submitRef = useRef<() => void>(() => undefined);
  useEffect(() => {
    if (mentions.length > 0) field.current?.focus();
    // Only on arrival: a chip added or removed later leaves the focus where the person put it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const defaultCwd = useDefaultCwd(machine);
  const cwd = machine ? startFolder(conversations, machine, defaultCwd ?? null) : null;
  const { choice, agents, onPick } = useStartChoice(machine);
  // Several models for one prompt (a beta function): the pill's choice and its companions.
  const { multiModel } = useBeta();
  const [more, setMore] = useState<IModelChoice[]>([]);
  const companions = useMemo(() => (multiModel ? more : []), [multiModel, more]);
  const multi = useMemo(() => {
    if (!multiModel || !choice) return undefined;
    const chosen = [choice, ...more];
    return {
      chosen,
      onToggle: (next: IModelChoice) => {
        const set = toggleChoice(chosen, next);
        if (choiceKey(set[0]) !== choiceKey(choice)) onPick(set[0]);
        setMore(set.slice(1));
      },
    };
  }, [multiModel, choice, more, onPick]);
  // Another model on the pill is the one model again; a change of depth keeps the companions.
  const onPickOne = (next: IModelChoice) => {
    if (!choice || choiceKey(next) !== choiceKey(choice)) setMore([]);
    onPick(next);
  };
  const limits = useAgentLimits(machine, limitsProviderOf(choice));
  // «/» on the start page: the commands that may start a chat, the model and depth of the pill, the earlier sessions.
  const slashActions = useMemo<ISlashActions>(() => ({ resume: () => requestUi(UI_REQUEST.resume) }), []);
  const slash = useSlashSource({
    machine,
    conv: null,
    provider: choice?.provider ?? null,
    cwd,
    actions: slashActions,
    onChat: noChat,
  });

  const onSubmit = useCallback(() => {
    if (creating) return;
    if (!machine || conversations === null) {
      if (!waiting.current) toast(t('next.newChat.connecting'), { tone: 'info' });
      const asked = read();
      // Typing on after the early Enter means the person is still writing: nothing goes out behind their back.
      waiting.current = () => {
        if (read() === asked) submitRef.current();
      };
      return;
    }
    let where: NewConversation['where'] | null = null;
    if (projectId) where = { projectId };
    else if (cwd) where = { cwd };
    if (!where) {
      toast(t('next.newChat.noFolder'), { tone: 'warning' });
      return;
    }
    if (uploads.uploading) {
      toast(t('next.composer.uploadingWait'), { tone: 'info' });
      return;
    }
    setCreating(true);
    if (choice) rememberChoice(choice);
    const on = machine;
    if (projectId && resolveProject) {
      resolveProject(projectId, on).then(
        (own) => send(on, { projectId: own }),
        (err: Error) => {
          setCreating(false);
          toast(t('next.newChat.failed'), { tone: 'error', description: err.message });
        },
      );
      return;
    }
    send(on, where);

    function send(target: MachineId, place: NewConversation['where']) {
      if (choice && companions.length > 0) {
        // One chat on each model, each in a worktree of its own, grouped under the prompt.
        const launch = { id: uuidv7(), prompt: launchPrompt(text) };
        const first = { text: withMentions(mentions, text.trim()), attachments: uploads.ready };
        const started = [choice, ...companions].map((pick) =>
          createConversation(target, {
            where: place,
            provider: pick.provider,
            ...(pick.profile ? { profile: pick.profile } : {}),
            model: pick.model ? { id: pick.model, effort: pick.effort, ultracode: pick.ultracode } : null,
            first,
            launch,
            worktree: true,
          }),
        );
        void Promise.all(
          started.map(async (one) => {
            const [st, id] = await Promise.all([one.done, one.conversation]);
            if (st.k === 'failed')
              toast(t('next.newChat.failed'), { tone: 'error', description: failureText(st.failure) });
            return id;
          }),
        ).then((ids) => {
          const opened = ids.find((id): id is ConvId => !!id);
          if (!opened) {
            setCreating(false);
            return;
          }
          clearDraft(NEW_CHAT);
          clearMentions(NEW_CHAT);
          uploads.reset();
          setMore([]);
          setCreating(false);
          onCreated(opened);
        });
        return;
      }
      const created = createConversation(target, {
        where: place,
        provider: choice?.provider ?? 'claude',
        ...(choice?.profile ? { profile: choice.profile } : {}),
        model: choice?.model
          ? { id: choice.model, effort: choice.effort, ultracode: choice.ultracode }
          : null,
        first: { text: withMentions(mentions, text.trim()), attachments: uploads.ready },
      });
      void created.done.then((st) => {
        if (st.k === 'failed') {
          setCreating(false);
          toast(t('next.newChat.failed'), {
            tone: 'error',
            description: failureText(st.failure),
          });
        }
      });
      void created.conversation.then((id) => {
        if (!id) return;
        clearDraft(NEW_CHAT);
        clearMentions(NEW_CHAT);
        uploads.reset();
        setCreating(false);
        onCreated(id);
      });
    }
  }, [
    machine,
    creating,
    cwd,
    projectId,
    text,
    mentions,
    uploads,
    onCreated,
    choice,
    companions,
    conversations,
    read,
    resolveProject,
  ]);
  useLayoutEffect(() => {
    submitRef.current = onSubmit;
  });
  const ready = machine !== null && conversations !== null;
  useEffect(() => {
    const send = waiting.current;
    waiting.current = null;
    if (ready) send?.();
  }, [ready]);

  const srcOf = useCallback((item: { id: string }) => (machine ? uploadSrc(machine, item) : null), [machine]);

  return (
    <AttachmentSrc.Provider value={srcOf}>
      <div className="hx-dock" data-creating={creating || undefined}>
        <div className="hx-enter" data-enter="up" data-eb="520" data-ed="700">
          <DockBar notice={voiceNotice} />
          <Composer
            enterSends={enterSends}
            value={text}
            onChange={setText}
            onSubmit={onSubmit}
            mode="new"
            onStop={() => Promise.resolve()}
            picker={
              choice && {
                value: choice,
                agents,
                locked: false,
                onChange: onPickOne,
                multi,
                machine: machines,
              }
            }
            slash={slash}
            quotes={mentions.map((m) => ({
              id: m.id,
              label: m.label,
              remark: false,
              icon: m.kind === MENTION_KIND.mr ? 'git-pull-request' : 'at-sign',
            }))}
            onRemoveQuote={(id) => removeMention(NEW_CHAT, id)}
            inputRef={field}
            stash={stash}
            takes={voice.onMic ? takes : null}
            attachments={uploads.attachments}
            onFiles={uploads.onFiles}
            onRemoveAttachment={uploads.onRemove}
            onRetryAttachment={uploads.onRetry}
            {...voice}
            limits={limits}
            maxText={machine ? limitsOf(machine).maxText : 0}
          />
        </div>
        {creating && (
          <div className="hx-hello__busy" role="status">
            {t('next.newChat.creating')}
          </div>
        )}
      </div>
    </AttachmentSrc.Provider>
  );
};

export default HomeComposer;
