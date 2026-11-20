// The quick voice settings over a button of the field (the mic and the wave): the button gets the gestures that open
// them (a right click, Shift+F10, a long press), the panel hangs on it. The button itself is drawn by the caller.
import { useState, type ReactNode } from 'react';
import DictationQuick from '@/next/features/voice-quick/dictation';
import LiveQuick from '@/next/features/voice-quick/live';
import QuickPanel from '@/next/features/voice-quick/panel';
import { useQuick, type IQuickButtonProps } from '@/next/features/voice-quick/use-quick';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

export const QUICK_KIND = { dictate: 'dictate', live: 'live' } as const;
export type TQuickKind = (typeof QUICK_KIND)[keyof typeof QUICK_KIND];

/** What the caller spreads on its button. */
export interface IQuickTrigger extends IQuickButtonProps {
  ref: (el: HTMLElement | null) => void;
  /** «Right click — settings», for the button's tooltip. */
  note: string;
}

interface IVoiceQuickProps {
  kind: TQuickKind;
  /** The voice mode's machine (the wave); the dictation takes the active one. */
  machine?: MachineId | null;
  /** A voice call is on. */
  inCall?: boolean;
  children: (trigger: IQuickTrigger) => ReactNode;
}

const VoiceQuick = ({ kind, machine = null, inCall = false, children }: IVoiceQuickProps) => {
  const quick = useQuick();
  // The panel is built the first time it is asked for (and stays, to leave with its animation): most buttons never open it.
  const [asked, setAsked] = useState(false);
  if (quick.open && !asked) setAsked(true);
  const label = t(kind === QUICK_KIND.live ? 'next.voice.quick.liveTitle' : 'next.voice.quick.dictateTitle');
  const close = () => quick.setOpen(false);
  return (
    <>
      {children({ ...quick.props, ref: quick.setAnchor, note: t('next.voice.quick.tip') })}
      {asked && (
        <QuickPanel quick={quick} label={label}>
          {kind === QUICK_KIND.live ? (
            <LiveQuick machine={machine} inCall={inCall} onNavigate={close} />
          ) : (
            <DictationQuick onNavigate={close} />
          )}
        </QuickPanel>
      )}
    </>
  );
};

export default VoiceQuick;
