import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import PromptEditor, { type PromptEditorHandle } from '@/components/workspace/prompt-editor';
import useIsMobileDevice from '@/hooks/use-is-mobile-device';
import useKeyboardViewport from '@/hooks/use-keyboard-viewport';
import useConfigStore from '@/hooks/use-config-store';
import type { IOwuiModel } from '@/lib/hl-openwebui';
import ModelPicker from './model-picker';
import EffortPicker from './effort-picker';
import VoiceMicButton from './voice-mic-button';
import VoiceRecorderBar from './voice-recorder-bar';
import ComposerSendButton, { SEND_KIND } from '@/components/agents/composer/composer-send-button';
import useVoiceComposer from '@/hooks/use-voice-composer';
import { dictationKeyAction } from '@/lib/dictation-intent';
import { t } from '@/lib/i18n';

interface IChatComposerProps {
  models: IOwuiModel[];
  model: IOwuiModel | undefined;
  effort: string | null;
  effortOptions: { efforts: string[]; defaultEffort: string | null };
  streaming: boolean;
  disabled?: boolean;
  savingParams?: boolean;
  autoFocus?: boolean;
  onSend: (text: string) => Promise<boolean>;
  onStop: () => void;
  onModelChange: (id: string) => void;
  onEffortChange: (effort: string | null) => void;
}

const ChatComposer = ({
  models,
  model,
  effort,
  effortOptions,
  streaming,
  disabled,
  savingParams,
  autoFocus,
  onSend,
  onStop,
  onModelChange,
  onEffortChange,
}: IChatComposerProps) => {
  const editorRef = useRef<PromptEditorHandle>(null);
  const [value, setValue] = useState('');
  const [sending, setSending] = useState(false);
  const isMobileDevice = useIsMobileDevice();
  useKeyboardViewport(isMobileDevice);
  const voiceAutoSend = useConfigStore((s) => s.voiceSettings.autoSend);
  useEffect(() => {
    if (autoFocus && !isMobileDevice) editorRef.current?.focus();
  }, [autoFocus, isMobileDevice]);

  const canSend = !disabled && !streaming && !sending && !!model && value.trim().length > 0;

  const submit = async (overrideText?: string) => {
    const text = overrideText ?? value;
    if (disabled || streaming || sending || !model || !text.trim()) return;
    setSending(true);
    setValue('');
    const accepted = await onSend(text).catch(() => false);
    if (!accepted) setValue(text);
    setSending(false);
    if (isMobileDevice) editorRef.current?.blur();
    else editorRef.current?.focus();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const action = dictationKeyAction(dictation.phase, event, isMobileDevice);
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
    if (event.key !== 'Enter' || event.shiftKey || isMobileDevice) return;
    event.preventDefault();
    void submit();
  };

  const dictation = useVoiceComposer({
    value,
    onValueChange: setValue,
    onAutoSend: (text) => void submit(text),
    disabled: disabled || streaming || sending,
  });

  return (
    <div className="mx-auto w-full max-w-content px-3 pb-3 sm:pb-4">
      <div className="agent-composer z-10" data-recording={dictation.phase ? '' : undefined}>
        <PromptEditor
          editorRef={editorRef}
          value={value}
          onValueChange={setValue}
          onKeyDown={handleKeyDown}
          placeholder={t('chat.chatComposer.placeholder')}
          aria-label={t('chat.chatComposer.input')}
          className="agent-prompt-editor tw-ui-editor-rows tw-composer-editor bg-transparent"
        />
        {dictation.voice.state === 'error' && dictation.voice.error && (
          <p className="tw-composer-note" data-tone="error" role="alert">
            {dictation.voice.error}
          </p>
        )}
        {dictation.phase ? (
          <VoiceRecorderBar dictation={dictation} sendsOnFinish={voiceAutoSend} />
        ) : (
          <div className="tw-cbar">
            <div className="tw-cbar-start">
              <ModelPicker
                models={models}
                value={model}
                disabled={disabled || streaming}
                onChange={onModelChange}
              />
              <EffortPicker
                efforts={effortOptions.efforts}
                defaultEffort={effortOptions.defaultEffort}
                value={effort}
                disabled={disabled || streaming || savingParams}
                onChange={onEffortChange}
              />
            </div>
            <div className="tw-cbar-end">
              <VoiceMicButton dictation={dictation} disabled={disabled || streaming} />
              {streaming ? (
                <ComposerSendButton
                  kind={SEND_KIND.stop}
                  label={t('chat.chatComposer.stop')}
                  onClick={onStop}
                />
              ) : (
                <ComposerSendButton disabled={!canSend} busy={sending} onClick={() => void submit()} />
              )}
            </div>
          </div>
        )}
      </div>
      <p className="mt-2 hidden text-center text-[10px] text-muted-foreground sm:block">
        {t('chat.chatComposer.keysHint')}
      </p>
    </div>
  );
};

export default ChatComposer;
