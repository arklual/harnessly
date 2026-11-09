// «Уведомления»: which events of a chat the person is not looking at come as a notice, where in the window the smart
// notice stands, which sounds play, and whether a tab in the background shows a notification of the system. This
// browser's, as the theme is; the browser's permission is asked from the switch, never on load. A sample shows the
// place at once, «Послушать» each sound.
import { Group, Row, SetButton, SwitchRow } from '@/next/features/settings/rows';
import Switch from '@/next/ui/switch';
import { t } from '@/next/i18n';
import {
  NOTICE_KINDS,
  NOTICE_PLACES,
  NOTICE_SOUNDS,
  type INotifySettings,
  type TNoticeKind,
  type TNoticePlace,
  type TNoticeSound,
} from '@/next/stores/notices';
import { SYSTEM_PERMISSION, type TSystemPermission } from '@/next/features/notifications/ping';

export interface INoticesSectionProps {
  settings: INotifySettings;
  /** What the browser says to notifications of the system. */
  permission: TSystemPermission;
  onKind: (kind: TNoticeKind, on: boolean) => void;
  onSound: (on: boolean) => void;
  onSoundOf: (sound: TNoticeSound, on: boolean) => void;
  /** Plays a sound once, to hear it. */
  onListen: (sound: TNoticeSound) => void;
  onSystem: (on: boolean) => void;
  onCards: (on: boolean) => void;
  onPlace: (place: TNoticePlace) => void;
  /** Shows a sample card where the cards stand now. */
  onSample: () => void;
}

const SYSTEM_HINT: Record<TSystemPermission, string> = {
  [SYSTEM_PERMISSION.granted]: 'next.notices.settings.system.hint',
  [SYSTEM_PERMISSION.ask]: 'next.notices.settings.system.hint',
  [SYSTEM_PERMISSION.denied]: 'next.notices.settings.system.denied',
  [SYSTEM_PERMISSION.none]: 'next.notices.settings.system.none',
};

/** The window in miniature with its six places; the chosen one is a card. */
const PlacePicker = ({
  value,
  disabled,
  onPick,
}: {
  value: TNoticePlace;
  disabled: boolean;
  onPick: (place: TNoticePlace) => void;
}) => (
  <div
    className="hx-place"
    role="radiogroup"
    aria-label={t('next.notices.settings.place')}
    aria-disabled={disabled || undefined}
  >
    {NOTICE_PLACES.map((place) => (
      <button
        key={place}
        type="button"
        role="radio"
        aria-checked={place === value}
        aria-label={t(`next.notices.settings.place.${place}`)}
        title={t(`next.notices.settings.place.${place}`)}
        className="hx-place__spot"
        data-at={place}
        disabled={disabled}
        onClick={() => place !== value && onPick(place)}
      />
    ))}
  </div>
);

const NoticesSection = ({
  settings,
  permission,
  onKind,
  onSound,
  onSoundOf,
  onListen,
  onSystem,
  onCards,
  onPlace,
  onSample,
}: INoticesSectionProps) => (
  <>
    <Group title={t('next.notices.settings.events')} foot={t('next.notices.settings.events.foot')}>
      {NOTICE_KINDS.map((kind) => (
        <SwitchRow
          key={kind}
          label={t(`next.notices.settings.kind.${kind}`)}
          hint={t(`next.notices.settings.kind.${kind}.hint`)}
          on={settings.kinds[kind]}
          onChange={(on) => onKind(kind, on)}
        />
      ))}
    </Group>
    <Group title={t('next.notices.settings.cards.title')}>
      <SwitchRow
        label={t('next.notices.settings.cards')}
        hint={t('next.notices.settings.cards.hint')}
        on={settings.cards}
        onChange={onCards}
      />
      <Row
        type="seg"
        label={t('next.notices.settings.place')}
        hint={t(`next.notices.settings.place.${settings.place}`)}
        off={!settings.cards}
      >
        <PlacePicker value={settings.place} disabled={!settings.cards} onPick={onPlace} />
      </Row>
      <Row
        type="link"
        label={t('next.notices.settings.sample')}
        hint={t('next.notices.settings.sample.hint')}
        off={!settings.cards}
      >
        <SetButton tone="ghost" icon="bell" disabled={!settings.cards} onClick={onSample}>
          {t('next.notices.settings.sample.show')}
        </SetButton>
      </Row>
    </Group>
    <Group title={t('next.notices.settings.how')}>
      <SwitchRow
        label={t('next.notices.settings.sound')}
        hint={t('next.notices.settings.sound.hint')}
        on={settings.sound}
        onChange={onSound}
      />
      {NOTICE_SOUNDS.map((sound) => (
        <Row
          key={sound}
          type="switch"
          label={t(`next.notices.settings.sound.${sound}`)}
          hint={t(`next.notices.settings.sound.${sound}.hint`)}
          off={!settings.sound}
        >
          <SetButton tone="ghost" icon="play" disabled={!settings.sound} onClick={() => onListen(sound)}>
            {t('next.notices.settings.sound.listen')}
          </SetButton>
          <Switch
            label={t(`next.notices.settings.sound.${sound}`)}
            checked={settings.sounds[sound]}
            disabled={!settings.sound}
            onChange={(on) => onSoundOf(sound, on)}
          />
        </Row>
      ))}
      <SwitchRow
        label={t('next.notices.settings.system')}
        hint={t(SYSTEM_HINT[permission])}
        on={settings.system && permission === SYSTEM_PERMISSION.granted}
        onChange={onSystem}
      />
    </Group>
  </>
);

export default NoticesSection;
