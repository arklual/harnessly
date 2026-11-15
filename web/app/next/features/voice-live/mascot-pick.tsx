// Settings → voice, «Человечек» (harnessly design project, «Человечек harnessly» → «Настройки голоса»): the voice agent's
// head, h (the default) or «Головка», side by side with a small living head in each. The person's setting: the same on
// every device, the field takes the new one at once.
import { useState } from 'react';
import { SegRow } from '@/next/features/settings/rows';
import { HEAD_KIND, type THeadKind } from '@/next/features/voice-live/api';
import VoiceHead, { HEAD } from '@/next/features/voice-live/voice-head';
import { t } from '@/next/i18n';
import { patchSettings, useVoiceMascot } from '@/next/stores/me';
import { toast } from '@/next/ui/toast';

const KINDS: readonly { kind: THeadKind; label: string }[] = [
  { kind: HEAD_KIND.h, label: 'next.chatgpt.voice.headH' },
  { kind: HEAD_KIND.face, label: 'next.chatgpt.voice.headFace' },
];

const MascotPick = () => {
  const kind = useVoiceMascot();
  const [busy, setBusy] = useState(false);
  const pick = (next: THeadKind) => {
    setBusy(true);
    patchSettings({ voiceMascot: next }).then(
      (res) => {
        setBusy(false);
        if (res.r !== 'accepted') toast(t('next.settings.failed'), { tone: 'error' });
      },
      () => {
        setBusy(false);
        toast(t('next.settings.failed'), { tone: 'error' });
      },
    );
  };
  return (
    <SegRow
      label={t('next.chatgpt.voice.head')}
      hint={t('next.chatgpt.voice.headHint')}
      value={kind}
      busy={busy}
      choices={KINDS.map((k) => ({
        value: k.kind,
        label: t(k.label),
        art: (
          <span className="hx-mascot-pick">
            <VoiceHead state={HEAD.idle} kind={k.kind} />
          </span>
        ),
      }))}
      onPick={pick}
    />
  );
};

export default MascotPick;
