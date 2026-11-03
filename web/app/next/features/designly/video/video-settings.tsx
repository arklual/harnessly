// The settings of a new video, behind the field's «Настройки»: the design system, the formats 16:9 / 9:16 / both, the
// length («Не важно», 15 s, 30 s, 1 min, 3 min) and the subtitles. The panel itself is create-panel.tsx.
import CreatePanel, { DsChoice } from '@/next/features/designly/create-panel';
import type { IDesignSystem } from '@/next/features/designly/api';
import {
  DEFAULT_VIDEO_SETTINGS,
  VIDEO_FORMATS_PICK,
  type IVideoCreateSettings,
  type TVideoFormatsPick,
} from '@/next/features/designly/video/video-create-settings';
import { VIDEO_FORMAT, VIDEO_SECONDS_CHOICES } from '@/next/features/designly/video/types';
import Segmented from '@/next/ui/segmented';
import Switch from '@/next/ui/switch';
import { t } from '@/next/i18n';

const FORMAT_LABEL = {
  landscape: VIDEO_FORMAT.landscape,
  portrait: VIDEO_FORMAT.portrait,
  both: null,
} as const satisfies Record<TVideoFormatsPick, string | null>;

/** A format choice as the button and the panel say it. */
export const formatLabel = (value: TVideoFormatsPick): string =>
  FORMAT_LABEL[value] ?? t('next.video.create.both');

const SECONDS_KEY = {
  15: 'next.video.create.sec15',
  30: 'next.video.create.sec30',
  60: 'next.video.create.sec60',
  180: 'next.video.create.sec180',
} as const;

/** A length as the button and the panel say it; null is «Не важно». */
export const secondsLabel = (seconds: number | null): string => {
  if (seconds === null) return t('next.video.create.anyLength');
  return t(SECONDS_KEY[seconds as keyof typeof SECONDS_KEY]);
};

/** What the settings button says: «Настройки» alone while all is as it starts, else what differs, joined by « · ». */
export const videoSettingsLabel = (s: IVideoCreateSettings, look: string | null): string => {
  const parts: string[] = [];
  if (look !== null) parts.push(look);
  if (s.formats !== DEFAULT_VIDEO_SETTINGS.formats) parts.push(formatLabel(s.formats));
  if (s.seconds !== null) parts.push(secondsLabel(s.seconds));
  if (s.subs !== DEFAULT_VIDEO_SETTINGS.subs)
    parts.push(s.subs ? t('next.video.create.subs') : t('next.video.create.noSubs'));
  return parts.length > 0 ? parts.join(' · ') : t('next.deck.create.settingsChip');
};

interface IVideoSettingsProps {
  open: boolean;
  onClose: () => void;
  anchor: HTMLElement | null;
  settings: IVideoCreateSettings;
  onChange: (next: IVideoCreateSettings) => void;
  systems: readonly IDesignSystem[];
  ds: string | null;
  onDs: (id: string | null) => void;
}

const VideoSettings = ({
  open,
  onClose,
  anchor,
  settings,
  onChange,
  systems,
  ds,
  onDs,
}: IVideoSettingsProps) => (
  <CreatePanel open={open} onClose={onClose} anchor={anchor} label={t('next.video.create.settings')}>
    <DsChoice systems={systems} value={ds} onChange={onDs} />
    <div className="hx-deck-set__label">{t('next.video.create.formats')}</div>
    <Segmented
      label={t('next.video.create.formats')}
      value={settings.formats}
      options={Object.values(VIDEO_FORMATS_PICK).map((value) => ({ value, label: formatLabel(value) }))}
      onChange={(formats) => onChange({ ...settings, formats })}
    />
    <div className="hx-deck-set__label">{t('next.video.create.length')}</div>
    <Segmented
      label={t('next.video.create.length')}
      value={settings.seconds === null ? 'any' : String(settings.seconds)}
      options={VIDEO_SECONDS_CHOICES.map((seconds) => ({
        value: seconds === null ? 'any' : String(seconds),
        label: secondsLabel(seconds),
      }))}
      onChange={(value) => onChange({ ...settings, seconds: value === 'any' ? null : Number(value) })}
    />
    <div className="hx-deck-set__tog">
      <span>{t('next.video.create.subs')}</span>
      <Switch
        label={t('next.video.create.subs')}
        checked={settings.subs}
        onChange={(subs) => onChange({ ...settings, subs })}
      />
    </div>
  </CreatePanel>
);

export default VideoSettings;
