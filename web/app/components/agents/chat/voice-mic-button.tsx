import { useState, type ReactNode } from 'react';
import { Check, ChevronDown, Mic, MicOff } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { TW_MENU_KIND, type TTwMenuEntry } from '@/components/agents/ui';
import VoiceKeyDialog, { useVoiceKeyLabel } from '@/components/agents/chat/voice-key-dialog';
import useConfigStore from '@/hooks/use-config-store';
import useMicrophoneDevices from '@/hooks/use-microphone-devices';
import type { IVoiceComposer } from '@/hooks/use-voice-composer';
import { preconnectVoiceSocket } from '@/lib/voice-engine-server';
import { VOICE_STATE } from '@/lib/voice-transcript';
import { cn } from '@/lib/utils';
import { t } from '@/lib/i18n';

const STATE_LABEL: Record<string, string> = {
  [VOICE_STATE.idle]: t('chat.voiceMicButton.idle'),
  [VOICE_STATE.requesting]: t('chat.voiceMicButton.requesting'),
  [VOICE_STATE.recording]: t('chat.voiceMicButton.recording'),
  [VOICE_STATE.transcribing]: t('chat.voiceMicButton.transcribing'),
  [VOICE_STATE.error]: t('chat.voiceMicButton.error'),
};

const UNSUPPORTED = t('chat.voiceMicButton.unsupported');

/** Microphone choice and «Hold to record» as menu rows: the tab composer keeps them in its «+» menu. */
export const useVoiceMenu = (): { entries: TTwMenuEntry[]; refresh: () => void; dialog: ReactNode } => {
  const settings = useConfigStore((s) => s.voiceSettings);
  const keyLabel = useVoiceKeyLabel();
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const setVoiceSettings = useConfigStore((s) => s.setVoiceSettings);
  const { devices, refresh } = useMicrophoneDevices();
  const entries: TTwMenuEntry[] = [
    {
      kind: TW_MENU_KIND.submenu,
      id: 'voice',
      label: t('chat.voiceMicButton.microphone'),
      icon: <Mic />,
      items: [
        {
          kind: TW_MENU_KIND.radio,
          id: 'voice-default',
          label: devices[0]
            ? t('chat.voiceMicButton.defaultDevice', { device: devices[0].label })
            : t('chat.voiceMicButton.default'),
          checked: !settings.deviceId,
          onSelect: () => setVoiceSettings({ deviceId: null }),
        },
        ...devices.map((device) => ({
          kind: TW_MENU_KIND.radio,
          id: `voice-${device.deviceId}`,
          label: device.label,
          checked: settings.deviceId === device.deviceId,
          onSelect: () => setVoiceSettings({ deviceId: device.deviceId }),
        })),
        { kind: TW_MENU_KIND.separator, id: 'voice-sep' },
        {
          kind: TW_MENU_KIND.checkbox,
          id: 'voice-hold',
          label: t('chat.voiceMicButton.hold'),
          hint:
            settings.mode === 'hold'
              ? t('chat.voiceMicButton.holdHint')
              : t('chat.voiceMicButton.toggleHint'),
          checked: settings.mode === 'hold',
          keepOpen: true,
          onSelect: () => setVoiceSettings({ mode: settings.mode === 'hold' ? 'toggle' : 'hold' }),
        },
        {
          kind: TW_MENU_KIND.action,
          id: 'voice-key',
          label: t('chat.voiceMicButton.dictationKey'),
          hint: keyLabel
            ? t('chat.voiceMicButton.keyHint', { key: keyLabel })
            : t('chat.voiceMicButton.keyNotSetHint'),
          onSelect: () => setKeyDialogOpen(true),
        },
      ],
    },
  ];
  return {
    entries,
    refresh,
    dialog: <VoiceKeyDialog open={keyDialogOpen} onClose={() => setKeyDialogOpen(false)} />,
  };
};

interface IVoiceMicButtonProps {
  dictation: IVoiceComposer;
  disabled?: boolean;
  /** A chevron with the microphone list and the hold switch; off where the composer has a «+» menu for them. */
  withSettings?: boolean;
}

/**
 * Mic button in the composer row. «Hold to record» records while the button is held (a quick
 * click keeps recording until «Done»); the toggle mode is a plain click. While dictating the row turns into
 * the recorder, so this button only starts recording and shows errors.
 */
const VoiceMicButton = ({ dictation, disabled, withSettings = true }: IVoiceMicButtonProps) => {
  const { voice } = dictation;
  const [open, setOpen] = useState(false);
  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const keyLabel = useVoiceKeyLabel();
  const settings = useConfigStore((s) => s.voiceSettings);
  const setVoiceSettings = useConfigStore((s) => s.setVoiceSettings);
  const { devices, refresh } = useMicrophoneDevices();

  const failed = voice.state === VOICE_STATE.error;
  const active = voice.state === VOICE_STATE.recording || voice.state === VOICE_STATE.requesting;
  const label = !voice.supported
    ? UNSUPPORTED
    : failed && voice.error
      ? voice.error
      : STATE_LABEL[voice.state];
  const hold = settings.mode === 'hold';
  const warmUp = () => {
    if (!disabled && voice.serverSupported && (settings.engine === 'auto' || settings.engine === 'server'))
      preconnectVoiceSocket();
  };

  return (
    <span className="tw-mic-wrap">
      <button
        type="button"
        className="tw-cbtn tw-mic"
        data-state={failed ? 'error' : active ? 'active' : undefined}
        disabled={disabled || !voice.supported}
        aria-label={label}
        aria-pressed={active}
        onPointerEnter={warmUp}
        onFocus={warmUp}
        title={hold && !failed ? t('chat.voiceMicButton.holdOrPress', { label }) : label}
        onPointerDown={(event) => {
          if (!hold || event.button !== 0) return;
          // Keeps the caret, and the phone keyboard, in the field.
          event.preventDefault();
          dictation.pressMic();
        }}
        onClick={(event) => {
          // A pointer press already started it in hold mode; the keyboard has no press, so its click does.
          if (hold && event.detail !== 0) return;
          dictation.pressMic();
        }}
      >
        {voice.supported ? <Mic aria-hidden="true" /> : <MicOff aria-hidden="true" />}
      </button>

      {withSettings && (
        <Popover
          open={open}
          onOpenChange={(next) => {
            setOpen(next);
            if (next) refresh();
          }}
        >
          <PopoverTrigger
            disabled={disabled}
            aria-label={t('chat.voiceMicButton.settings')}
            title={t('chat.voiceMicButton.settings')}
            className="tw-mic-chevron"
          >
            <ChevronDown aria-hidden="true" />
          </PopoverTrigger>
          <PopoverContent
            side="top"
            align="end"
            sideOffset={10}
            className="w-[260px] max-w-[calc(100vw-24px)] gap-0 rounded-2xl p-2"
          >
            <div role="group" aria-label={t('chat.voiceMicButton.microphone')} className="pb-1">
              <div className="px-2.5 pb-2 pt-2 text-xs font-semibold">
                {t('chat.voiceMicButton.microphone')}
              </div>
              <button
                type="button"
                role="menuitemradio"
                aria-checked={!settings.deviceId}
                onClick={() => setVoiceSettings({ deviceId: null })}
                className="flex min-h-10 w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted"
              >
                <span className="flex-1 truncate">
                  {devices[0]
                    ? t('chat.voiceMicButton.defaultDevice', { device: devices[0].label })
                    : t('chat.voiceMicButton.default')}
                </span>
                {!settings.deviceId && <Check size={14} className="text-brand" />}
              </button>
              {devices.map((device) => (
                <button
                  key={device.deviceId}
                  type="button"
                  role="menuitemradio"
                  aria-checked={settings.deviceId === device.deviceId}
                  onClick={() => setVoiceSettings({ deviceId: device.deviceId })}
                  className="flex min-h-10 w-full items-center gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted"
                >
                  <span className="flex-1 truncate">{device.label}</span>
                  {settings.deviceId === device.deviceId && <Check size={14} className="text-brand" />}
                </button>
              ))}
            </div>
            <div className="border-t pt-1">
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setOpen(false);
                  setKeyDialogOpen(true);
                }}
                className="flex min-h-10 w-full items-center justify-between gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted"
              >
                <span>{t('chat.voiceMicButton.dictationKey')}</span>
                <span className="truncate text-xs text-muted-foreground">
                  {keyLabel ?? t('chat.voiceMicButton.notSet')}
                </span>
              </button>
              <button
                type="button"
                role="menuitemcheckbox"
                aria-checked={hold}
                onClick={() => setVoiceSettings({ mode: hold ? 'toggle' : 'hold' })}
                className="flex min-h-10 w-full items-center justify-between gap-2.5 rounded-xl px-2.5 py-2 text-left text-sm transition-colors hover:bg-muted"
              >
                <span>{t('chat.voiceMicButton.hold')}</span>
                <span
                  className={cn(
                    'flex h-5 w-9 shrink-0 items-center rounded-full px-0.5 transition-colors',
                    hold ? 'justify-end bg-brand' : 'justify-start bg-muted-foreground/25',
                  )}
                >
                  <span className="h-4 w-4 rounded-full bg-background shadow" />
                </span>
              </button>
            </div>
          </PopoverContent>
        </Popover>
      )}
      <VoiceKeyDialog open={keyDialogOpen} onClose={() => setKeyDialogOpen(false)} />
    </span>
  );
};

export default VoiceMicButton;
