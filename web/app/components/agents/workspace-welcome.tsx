import { appConfig } from '@/platform/config';
import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent } from 'react';
import dynamic from 'next/dynamic';
import { FolderOpen } from 'lucide-react';
import PromptEditor, { type PromptEditorHandle } from '@/components/workspace/prompt-editor';
import ProviderIcon from '@/components/agents/provider-icon';
import WelcomeSlashSlot, { type TWelcomeKeyHandler } from '@/components/agents/welcome-slash-slot';
import { LaunchOptionsPicker } from '@/components/agents/launch-options-picker';
import { TwSegmented, TwSelect, type ITwChoice, type ITwOption } from '@/components/agents/ui';
import VoiceMicButton from '@/components/agents/chat/voice-mic-button';
import VoiceRecorderBar, { VoiceLiveDraft } from '@/components/agents/chat/voice-recorder-bar';
import useIsMobileDevice from '@/hooks/use-is-mobile-device';
import ComposerSendButton from '@/components/agents/composer/composer-send-button';
import useVoiceComposer from '@/hooks/use-voice-composer';
import { dictationKeyAction } from '@/lib/dictation-intent';
import useConfigStore from '@/hooks/use-config-store';
import useLaunchOptions from '@/hooks/use-launch-options';
import { cleanConversationTitle, type Conversation } from '@/lib/hl-conversations';
import ContextChipRow from '@/components/agents/context-chip-row';
import ContextPickerPopover from '@/components/agents/context-picker-popover';
import useContextPicker from '@/hooks/use-context-picker';
import { appendContextBlock } from '@/lib/hl-context-chips';
import {
  newConversationOptions,
  type IStartOptions,
  type TNewConversationKind,
} from '@/lib/hl-new-conversation';
import type { IMachineFeatures } from '@/types/server/hl-machines';
import type { IRuntimeWorkspace } from '@/hooks/use-hl-conversations';
import { isAutoLaunch, type TAgentProvider } from '@/types/hl-launch';
import type { IPendingLaunch } from '@/lib/hl-pending-launch';
import { t } from '@/lib/i18n';

// The timeline carries markdown and syntax highlighting: it loads after the first screen, not with it.
const loadTimeline = () => import('@/components/timeline/timeline-view');
const LaunchPreview = dynamic(() => loadTimeline().then((m) => m.LaunchPreview), { ssr: false });

const STARTERS: Record<TNewConversationKind, readonly string[]> = {
  claude: [
    t('agents.workspaceWelcome.starterClaudeBuild'),
    t('agents.workspaceWelcome.starterClaudeWeek'),
    t('agents.workspaceWelcome.starterClaudeTests'),
  ],
  codex: [
    t('agents.workspaceWelcome.starterCodexTests'),
    t('agents.workspaceWelcome.starterCodexReview'),
    t('agents.workspaceWelcome.starterCodexExplain'),
  ],
  chat: [
    t('agents.workspaceWelcome.starterChatLetter'),
    t('agents.workspaceWelcome.starterChatCompare'),
    t('agents.workspaceWelcome.starterChatSimple'),
  ],
};

const PLACEHOLDERS: Record<TNewConversationKind, string> = {
  claude: t('agents.workspaceWelcome.placeholderAgent'),
  codex: t('agents.workspaceWelcome.placeholderAgent'),
  chat: t('agents.workspaceWelcome.placeholderChat'),
};

/** The chat picks its model inside, the agents start with their own. */
const LAUNCH_PROVIDER = {
  claude: 'claude',
  codex: 'codex',
  chat: null,
} as const satisfies Record<TNewConversationKind, TAgentProvider | null>;

const subscribeNever = () => () => {};
const currentHour = () => new Date().getHours();
const noHourOnServer = () => null;

const greeting = (hour: number) => {
  if (hour < 5) return t('agents.workspaceWelcome.greetingNight');
  if (hour < 12) return t('agents.workspaceWelcome.greetingMorning');
  if (hour < 18) return t('agents.workspaceWelcome.greetingDay');
  return t('agents.workspaceWelcome.greetingEvening');
};

interface IWorkspaceWelcomeProps {
  name: string;
  busy: boolean;
  recent: Conversation[];
  /** The list is still loading: «Continue» keeps its place, so the screen does not jump up when it arrives. */
  recentLoading?: boolean;
  /** Machine new conversations go to, and its features; `null` while unknown hides gated options. */
  machineId?: string;
  features: IMachineFeatures | null;
  /** Workspaces (directories) of that machine; a choice appears when there are several. */
  workspaces: readonly IRuntimeWorkspace[];
  /** A kind and a text to put into the composer (a failed start brings its message back); `seq` repeats the request. */
  composeRequest?: { kind: TNewConversationKind; seq: number; text?: string } | null;
  onStart: (kind: TNewConversationKind, prompt: string, options: IStartOptions) => void;
  onSelect: (item: Conversation) => void;
}

const DEFAULT_WORKSPACE = 'default';
const RECENT_SLOTS = ['a', 'b', 'c', 'd'] as const;

const WorkspaceWelcome = ({
  name,
  busy,
  recent,
  recentLoading = false,
  machineId,
  features,
  workspaces,
  composeRequest = null,
  onStart,
  onSelect,
}: IWorkspaceWelcomeProps) => {
  const editorRef = useRef<PromptEditorHandle>(null);
  const slashKeyRef = useRef<TWelcomeKeyHandler>(null);
  const [chosen, setChosen] = useState<TNewConversationKind>(composeRequest?.kind ?? 'claude');
  const [value, setValue] = useState(composeRequest?.text ?? '');
  const [chosenWorkspace, setChosenWorkspace] = useState<string>(DEFAULT_WORKSPACE);
  // While the conversation is being created its first message is already on screen, as it will be there.
  const [launching, setLaunching] = useState<(IPendingLaunch & { provider: 'claude' | 'codex' }) | null>(
    null,
  );
  const [appliedCompose, setAppliedCompose] = useState(composeRequest);
  const [wasBusy, setWasBusy] = useState(busy);
  if (busy !== wasBusy) {
    setWasBusy(busy);
    if (!busy) setLaunching(null);
  }
  if (composeRequest !== appliedCompose) {
    setAppliedCompose(composeRequest);
    if (composeRequest) setChosen(composeRequest.kind);
    if (composeRequest?.text) setValue(composeRequest.text);
  }
  const hour = useSyncExternalStore(subscribeNever, currentHour, noHourOnServer);
  const options = newConversationOptions(features);
  // A kind the machine does not offer (after switching machines) falls back to the first option on screen.
  const kind = options.some((option) => option.kind === chosen) ? chosen : options[0].kind;
  const launchProvider = LAUNCH_PROVIDER[kind];
  const launch = useLaunchOptions(launchProvider, machineId);
  // The saved choice has to be known before starting, or «Auto» would stand in for it unnoticed; a failed load says so on the chip.
  const launchReady =
    !launchProvider || ((launch.options !== null || launch.error !== null) && launch.pending === null);
  const saved = launch.options?.saved ?? null;
  const kindChoices: ITwChoice<TNewConversationKind>[] = options
    .filter((option) => option.kind !== 'chat' || appConfig().features.chats)
    .map((option) => ({
      value: option.kind,
      label: option.title,
      icon: <ProviderIcon provider={option.kind} size="sm" />,
    }));

  useEffect(() => {
    editorRef.current?.focus();
  }, []);

  // Ready before the first message is sent, once the screen has settled.
  useEffect(() => {
    if (typeof requestIdleCallback !== 'function') return;
    const id = requestIdleCallback(() => void loadTimeline(), { timeout: 3000 });
    return () => cancelIdleCallback(id);
  }, []);

  const workspaceChoice = launchProvider && workspaces.length > 1;
  // A workspace of the previous machine does not exist on this one: its default stands in.
  const workspaceId = workspaces.some((workspace) => workspace.id === chosenWorkspace)
    ? chosenWorkspace
    : DEFAULT_WORKSPACE;
  const workspaceOptions: ITwOption[] = [
    { value: DEFAULT_WORKSPACE, label: t('agents.workspaceWelcome.defaultFolder'), icon: <FolderOpen /> },
    ...workspaces.map((workspace) => ({ value: workspace.id, label: workspace.name, icon: <FolderOpen /> })),
  ];
  const startOptions = (): IStartOptions => ({
    ...(launchProvider && saved && !isAutoLaunch(saved) ? { launch: saved } : {}),
    ...(workspaceChoice && workspaceId !== DEFAULT_WORKSPACE ? { workspaceId } : {}),
  });
  const canSend = value.trim().length > 0 && !busy && launchReady;
  const contextPicker = useContextPicker({ value, setValue, cwd: null, recentConversations: recent });
  const runCommand = (line: string) => {
    if (busy || !launchReady) return;
    if (kind === 'claude' || kind === 'codex') setLaunching({ prompt: '', at: Date.now(), provider: kind });
    onStart(kind, line, { ...startOptions(), firstLine: line });
  };
  const submit = (overrideText?: string) => {
    const text = (overrideText ?? value).trim();
    if (!text || busy || !launchReady) return;
    // A command line goes the command way from the send button too: the conversation's composer runs it.
    if (launchProvider && /^\/\S/.test(text) && !text.includes('\n')) return runCommand(text);
    const finalText = contextPicker.chips.length ? appendContextBlock(text, contextPicker.chips) : text;
    if (kind === 'claude' || kind === 'codex')
      setLaunching({ prompt: finalText, at: Date.now(), provider: kind });
    onStart(kind, finalText, startOptions());
    if (contextPicker.chips.length) contextPicker.clearChips();
  };
  const isMobileDevice = useIsMobileDevice();
  const dictation = useVoiceComposer({
    value,
    onValueChange: setValue,
    onAutoSend: submit,
    disabled: busy || !launchReady,
  });
  const voiceAutoSend = useConfigStore((s) => s.voiceSettings.autoSend);
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (slashKeyRef.current?.(event)) return;
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
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      submit();
    }
  };

  if (busy && launching) {
    return (
      <div className="tw-welcome tw-welcome-launching">
        <LaunchPreview launch={launching} provider={launching.provider} />
      </div>
    );
  }

  // A phone has no room for four agents beside the model in the row: they sit above the field, with their names.
  const kindPicker = (
    <TwSegmented
      aria-label={t('agents.workspaceWelcome.kindAria')}
      options={kindChoices}
      value={kind}
      onChange={setChosen}
      collapseLabels={!isMobileDevice}
      className={isMobileDevice ? 'tw-welcome-kinds' : undefined}
    />
  );

  return (
    <div className="tw-welcome" data-testid="home">
      <div className="tw-welcome-glow" aria-hidden="true" />
      <div className="tw-welcome-inner">
        <span className="tw-welcome-mark" aria-hidden="true">
          <ProviderIcon provider={kind} size="lg" />
        </span>
        <h1 className="tw-welcome-title">
          {hour === null ? t('agents.workspaceWelcome.hello') : greeting(hour)}
          {name ? `, ${name}` : ''}
        </h1>
        <p className="tw-welcome-sub">{t('agents.workspaceWelcome.subtitle')}</p>

        {isMobileDevice && kindPicker}
        <div
          className="tw-welcome-composer agent-composer"
          data-testid="home-composer"
          data-recording={dictation.phase ? '' : undefined}
        >
          <WelcomeSlashSlot
            kind={kind}
            machineId={machineId}
            value={value}
            onValueChange={setValue}
            editorRef={editorRef}
            keyHandlerRef={slashKeyRef}
            onRunCommand={runCommand}
          />
          <ContextPickerPopover picker={contextPicker} />
          <ContextChipRow chips={contextPicker.chips} onRemove={contextPicker.removeChip} />
          <PromptEditor
            editorRef={editorRef}
            value={value}
            onValueChange={setValue}
            onKeyDown={onKeyDown}
            placeholder={PLACEHOLDERS[kind]}
            aria-label={t('agents.workspaceWelcome.firstMessage')}
            aria-expanded={contextPicker.open}
            aria-controls={contextPicker.open ? contextPicker.listId : undefined}
            aria-activedescendant={contextPicker.activeDescendant}
            className={`agent-prompt-editor tw-ui-editor-rows tw-composer-editor tw-welcome-editor${dictation.preview ? ' tw-editor-behind-draft' : ''}`}
          />
          {dictation.preview && (
            <VoiceLiveDraft
              value={value}
              preview={dictation.preview}
              className="agent-prompt-editor tw-ui-editor-rows tw-composer-editor tw-welcome-editor"
            />
          )}
          {dictation.voice.state === 'error' && dictation.voice.error && (
            <p className="tw-composer-note" data-tone="error" role="alert">
              {dictation.voice.error}
            </p>
          )}
          {dictation.phase ? (
            <VoiceRecorderBar dictation={dictation} sendsOnFinish={voiceAutoSend} />
          ) : (
            <div className="tw-cbar tw-welcome-toolbar">
              <div className="tw-cbar-start">
                <div className="tw-welcome-controls">
                  {!isMobileDevice && kindPicker}
                  {launchProvider && <LaunchOptionsPicker provider={launchProvider} launch={launch} />}
                  {workspaceChoice && (
                    <TwSelect
                      aria-label={t('agents.workspaceWelcome.folderAria')}
                      options={workspaceOptions}
                      value={workspaceId}
                      onChange={setChosenWorkspace}
                      side="top"
                      className="tw-welcome-workspace"
                      title={t('agents.workspaceWelcome.folderTitle')}
                    />
                  )}
                </div>
              </div>
              <div className="tw-cbar-end">
                <VoiceMicButton
                  dictation={dictation}
                  disabled={busy || !launchReady}
                  withSettings={!isMobileDevice}
                />
                <ComposerSendButton
                  testId="home-send"
                  disabled={!canSend}
                  busy={busy}
                  onClick={() => submit()}
                />
              </div>
            </div>
          )}
        </div>

        <div className="tw-starters">
          {STARTERS[kind].map((text) => (
            <button
              key={text}
              type="button"
              className="tw-starter"
              onClick={() => {
                setValue(text);
                editorRef.current?.focusEnd();
              }}
            >
              {text}
            </button>
          ))}
        </div>

        {recent.length === 0 && recentLoading && (
          <div className="tw-recent" aria-hidden="true">
            <h2>{t('agents.workspaceWelcome.continue')}</h2>
            <div className="tw-recent-grid">
              {RECENT_SLOTS.map((slot) => (
                <span key={slot} className="tw-recent-card tw-recent-card-skeleton">
                  <span className="tw-skeleton" />
                </span>
              ))}
            </div>
          </div>
        )}
        {recent.length > 0 && (
          <div className="tw-recent tw-recent-ready">
            <h2>{t('agents.workspaceWelcome.continue')}</h2>
            <div className="tw-recent-grid">
              {recent.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  className="tw-recent-card"
                  onClick={() => onSelect(item)}
                >
                  <ProviderIcon provider={item.provider} size="sm" />
                  <span>{cleanConversationTitle(item.title)}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
};

export default WorkspaceWelcome;
