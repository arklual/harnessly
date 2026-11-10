// «Уведомления»: where in the window the smart notice stands (the window in miniature, the chosen place a black pill),
// what comes as one, which sounds play, whether a tab in the background shows a notification of the system, and the key
// that opens it. This browser's, as the theme is; the browser's permission is asked from the switch, never on load. A
// sample shows the place at once, ▶ plays each sound.
import { Group, Row, SetButton, SwitchRow } from '@/next/features/settings/rows';
import { SYSTEM_PERMISSION, type TSystemPermission } from '@/next/features/notifications/ping';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';
import {
  NOTICE_PLACES,
  NOTICE_SOUNDS,
  type INotifySettings,
  type TNoticePlace,
  type TNoticeSound,
} from '@/next/stores/notices';

/** What the page shows as one switch: «Сломалось» is a failed turn and a turn that hit the limit, «Машина» a machine
 *  that cannot cope. */
export const NOTICE_SHOW = ['waiting', 'broken', 'done', 'machine'] as const;
export type TNoticeShow = (typeof NOTICE_SHOW)[number];

/** The places in three choices: by the field (the middle of the bottom edge), along the top, in a bottom corner. */
const PLACE_GROUPS = {
  field: ['bottom-center'],
  top: ['top-left', 'top-center', 'top-right'],
  bottom: ['bottom-left', 'bottom-right'],
} as const satisfies Record<string, readonly TNoticePlace[]>;
type TPlaceGroup = keyof typeof PLACE_GROUPS;
const PLACE_GROUP_ORDER = ['field', 'top', 'bottom'] as const satisfies readonly TPlaceGroup[];
/** Where a group puts the notice when it is chosen from the list. */
const GROUP_FIRST: Record<TPlaceGroup, TNoticePlace> = {
  field: 'bottom-center',
  top: 'top-center',
  bottom: 'bottom-right',
};

const groupOf = (place: TNoticePlace): TPlaceGroup =>
  PLACE_GROUP_ORDER.find((g) => (PLACE_GROUPS[g] as readonly TNoticePlace[]).includes(place)) ?? 'field';

export interface INoticesSectionProps {
  settings: INotifySettings;
  /** What the browser says to notifications of the system. */
  permission: TSystemPermission;
  /** The key that opens the smart notice as this keyboard writes it; null when the person took it away. */
  noticeKey: string | null;
  onShow: (show: TNoticeShow, on: boolean) => void;
  /** A finished turn comes with a few lines on what was done, from the cheapest model. */
  onSummary: (on: boolean) => void;
  onSound: (on: boolean) => void;
  onSoundOf: (sound: TNoticeSound, on: boolean) => void;
  /** Plays a sound once, to hear it. */
  onListen: (sound: TNoticeSound) => void;
  onSystem: (on: boolean) => void;
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

/** Whether a switch of the page is on: «Сломалось» is on while either of its kinds is. */
export const showOn = (settings: INotifySettings, show: TNoticeShow): boolean => {
  if (show === 'broken') return settings.kinds.failed || settings.kinds.limit;
  if (show === 'machine') return settings.machine;
  return settings.kinds[show];
};

/** The window in miniature — the left column, the field — with its six places; the chosen one is the black pill. */
const PlaceScheme = ({ value, onPick }: { value: TNoticePlace; onPick: (place: TNoticePlace) => void }) => (
  <div className="hx-nplace__scheme" role="radiogroup" aria-label={t('next.notices.settings.spot')}>
    <span className="hx-nplace__rail" aria-hidden="true" />
    <span className="hx-nplace__field" aria-hidden="true" />
    {NOTICE_PLACES.map((place) => (
      <button
        key={place}
        type="button"
        role="radio"
        aria-checked={place === value}
        aria-label={t(`next.notices.settings.place.${place}`)}
        title={t(`next.notices.settings.place.${place}`)}
        className="hx-nplace__spot"
        data-at={place}
        onClick={() => place !== value && onPick(place)}
      />
    ))}
  </div>
);

const PlaceChoice = ({ value, onPick }: { value: TNoticePlace; onPick: (place: TNoticePlace) => void }) => {
  const chosen = groupOf(value);
  return (
    <div className="hx-nplace">
      <PlaceScheme value={value} onPick={onPick} />
      <div className="hx-nplace__list" role="radiogroup" aria-label={t('next.notices.settings.place')}>
        {PLACE_GROUP_ORDER.map((group) => {
          const on = group === chosen;
          return (
            <button
              key={group}
              type="button"
              role="radio"
              aria-checked={on}
              className="hx-nplace__opt"
              onClick={() => !on && onPick(GROUP_FIRST[group])}
            >
              <span className="hx-nplace__radio" aria-hidden="true" />
              <span className="hx-nplace__opt-label">{t(`next.notices.settings.where.${group}`)}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
};

/** A sound's row: ▶ before its words, its switch after them. */
const SoundRow = ({
  sound,
  on,
  off,
  onListen,
  onChange,
}: {
  sound: TNoticeSound;
  on: boolean;
  off: boolean;
  onListen: () => void;
  onChange: (on: boolean) => void;
}) => {
  const label = t(`next.notices.settings.sound.${sound}`);
  return (
    <div className="hx-set__row" data-type="switch" data-off={off || undefined}>
      <button
        type="button"
        className="hx-nplace__play"
        aria-label={t('next.notices.settings.sound.listen', { name: label })}
        title={t('next.notices.settings.sound.listen', { name: label })}
        disabled={off}
        onClick={onListen}
      >
        <Icon name="play" size="sm" />
      </button>
      <div className="hx-set__row-text">
        <span className="hx-set__row-label">{label}</span>
        <span className="hx-set__row-hint">{t(`next.notices.settings.sound.${sound}.hint`)}</span>
      </div>
      <Switch label={label} checked={on} disabled={off} onChange={onChange} />
    </div>
  );
};

const NoticesSection = ({
  settings,
  permission,
  noticeKey,
  onShow,
  onSummary,
  onSound,
  onSoundOf,
  onListen,
  onSystem,
  onPlace,
  onSample,
}: INoticesSectionProps) => (
  <>
    <Group title={t('next.notices.settings.where')}>
      <PlaceChoice value={settings.place} onPick={onPlace} />
      <Row type="link" label={t('next.notices.settings.look')} hint={t('next.notices.settings.look.hint')}>
        <SetButton tone="ghost" onClick={onSample}>
          {t('next.notices.settings.look.show')}
        </SetButton>
      </Row>
    </Group>
    <Group title={t('next.notices.settings.what')}>
      {NOTICE_SHOW.map((show) => (
        <SwitchRow
          key={show}
          label={t(`next.notices.settings.show.${show}`)}
          hint={t(`next.notices.settings.show.${show}.hint`)}
          on={showOn(settings, show)}
          onChange={(on) => onShow(show, on)}
        />
      ))}
      <SwitchRow
        label={t('next.notices.settings.summary')}
        hint={t('next.notices.settings.summary.hint')}
        on={settings.summary}
        onChange={onSummary}
      />
    </Group>
    <Group title={t('next.notices.settings.sounds')}>
      <SwitchRow label={t('next.notices.settings.sound')} on={settings.sound} onChange={onSound} />
      {NOTICE_SOUNDS.map((sound) => (
        <SoundRow
          key={sound}
          sound={sound}
          on={settings.sounds[sound]}
          off={!settings.sound}
          onListen={() => onListen(sound)}
          onChange={(on) => onSoundOf(sound, on)}
        />
      ))}
    </Group>
    <Group title={t('next.notices.settings.hidden')}>
      <SwitchRow
        label={t('next.notices.settings.system')}
        hint={t(SYSTEM_HINT[permission])}
        on={settings.system && permission === SYSTEM_PERMISSION.granted}
        onChange={onSystem}
      />
    </Group>
    {/* A phone has no keyboard: the key is shown where one is. */}
    <div className="hx-nplace__keys">
      <Group title={t('next.notices.settings.key')}>
        <Row
          type="link"
          label={t('next.notices.settings.key.open')}
          hint={t('next.notices.settings.key.open.hint')}
        >
          {noticeKey ? (
            <kbd className="hx-keycap hx-nplace__key">{noticeKey}</kbd>
          ) : (
            <span className="hx-set__row-hint">{t('next.notices.settings.key.none')}</span>
          )}
        </Row>
      </Group>
    </div>
  </>
);

export default NoticesSection;
