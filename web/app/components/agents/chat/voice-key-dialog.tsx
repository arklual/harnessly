import { useCallback } from 'react';
import { BUTTON_VARIANT, DIALOG_SIZE, TwButton, TwDialog } from '@/components/agents/ui';
import useConfigStore from '@/hooks/use-config-store';
import useVoiceKeyCapture from '@/hooks/use-voice-key-capture';
import { isMac } from '@/lib/keyboard-shortcuts';
import { voiceKeyLabel } from '@/lib/voice-key';
import { t } from '@/lib/i18n';

/** The dictation key as the menus show it. */
export const useVoiceKeyLabel = (): string | null => {
  const key = useConfigStore((s) => s.voiceSettings.key);
  return key ? voiceKeyLabel(key, isMac) : null;
};

interface IVoiceKeyDialogProps {
  open: boolean;
  onClose: () => void;
}

/**
 * «Dictation key»: press «Assign», press the key. A tap of that key then starts dictation in the
 * composer and the next tap ends it, without holding.
 */
const VoiceKeyDialog = ({ open, onClose }: IVoiceKeyDialogProps) => {
  const label = useVoiceKeyLabel();
  const setVoiceSettings = useConfigStore((s) => s.setVoiceSettings);
  const assign = useCallback((key: string) => setVoiceSettings({ key }), [setVoiceSettings]);
  const { capturing, refused, start, cancel } = useVoiceKeyCapture(assign);
  const close = () => {
    cancel();
    onClose();
  };

  return (
    <TwDialog
      open={open}
      onClose={close}
      title={t('chat.voiceKeyDialog.title')}
      description={t('chat.voiceKeyDialog.description')}
      size={DIALOG_SIZE.sm}
      // While a key is awaited Escape belongs to the capture, not to the dialog.
      dismissible={!capturing}
      footer={<TwButton onClick={close}>{t('chat.voiceKeyDialog.done')}</TwButton>}
    >
      <div className="tw-voice-key">
        <output className="tw-voice-key-value" data-capturing={capturing || undefined} aria-live="polite">
          {capturing ? t('chat.voiceKeyDialog.pressKey') : (label ?? t('chat.voiceKeyDialog.notSet'))}
        </output>
        <div className="tw-voice-key-actions">
          <TwButton
            variant={capturing ? BUTTON_VARIANT.ghost : BUTTON_VARIANT.primary}
            onClick={capturing ? cancel : start}
          >
            {capturing
              ? t('chat.voiceKeyDialog.cancel')
              : label
                ? t('chat.voiceKeyDialog.change')
                : t('chat.voiceKeyDialog.assign')}
          </TwButton>
          {label && !capturing && (
            <TwButton variant={BUTTON_VARIANT.ghost} onClick={() => setVoiceSettings({ key: null })}>
              {t('chat.voiceKeyDialog.reset')}
            </TwButton>
          )}
        </div>
        <p className="tw-voice-key-note" role={refused ? 'alert' : undefined}>
          {refused ? t('chat.voiceKeyDialog.refused') : t('chat.voiceKeyDialog.hint')}
        </p>
      </div>
    </TwDialog>
  );
};

export default VoiceKeyDialog;
