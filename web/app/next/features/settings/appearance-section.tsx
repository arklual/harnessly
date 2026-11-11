// «Оформление»: the look of the app in this browser — light, dark or as the system has it, the palette over it, and the
// text size and the notices' colour (the person's settings, on every device). Every choice is a small picture of the app in that look, drawn
// with the same tokens the app wears (.hx-look in palettes.css), so a palette is seen before it is picked; a pick
// repaints the app from the card that was pressed.
import type { MouseEvent } from 'react';
import { Group, SegRow } from '@/next/features/settings/rows';
import { THEMES, TEXT_SIZES, type ThemeChoice } from '@/next/features/settings/general-section';
import { t } from '@/next/i18n';
import { NOTICE_COLORS, type PersonSettings, type TNoticeColor } from '@/next/stores/me';
import Icon from '@/next/ui/icon';
import { PALETTES, type IChangeOrigin, type TPalette } from '@/platform/theme';

const THEME_ICON = { light: 'sun', dark: 'moon', system: 'monitor' } as const satisfies Record<
  ThemeChoice,
  string
>;

/** The middle of the pressed card: the new look spreads from there. */
const originOf = (event: MouseEvent<HTMLElement>): IChangeOrigin => {
  const box = event.currentTarget.getBoundingClientRect();
  return { x: box.left + box.width / 2, y: box.top + box.height / 2 };
};

/** A window of the app in miniature: the sidebar with its rows, a message, the agent's answer and an accent button. */
const Miniature = ({ mode, palette }: { mode: 'light' | 'dark'; palette: TPalette }) => (
  <span className="hx-look" data-mode={mode} data-palette={palette} aria-hidden="true">
    <span className="hx-look__rail">
      <i className="hx-look__dot" />
      <i className="hx-look__bar" />
      <i className="hx-look__bar hx-look__bar--short" />
      <i className="hx-look__bar" />
    </span>
    <span className="hx-look__win">
      <i className="hx-look__bubble" />
      <i className="hx-look__line" />
      <i className="hx-look__line hx-look__line--short" />
      <i className="hx-look__accent" />
    </span>
  </span>
);

export interface IAppearanceSectionProps {
  theme: ThemeChoice;
  /** The light or the dark the app wears now (the system's, for «как в системе»). */
  mode: 'light' | 'dark';
  palette: TPalette;
  textSize: PersonSettings['textSize'];
  /** The text size waits for the server. */
  textBusy: boolean;
  noticeColor: TNoticeColor;
  /** The notices' colour waits for the server. */
  noticeBusy: boolean;
  onTheme: (theme: ThemeChoice, from: IChangeOrigin) => void;
  onPalette: (palette: TPalette, from: IChangeOrigin) => void;
  onTextSize: (size: PersonSettings['textSize']) => void;
  onNoticeColor: (color: TNoticeColor) => void;
}

const AppearanceSection = ({
  theme,
  mode,
  palette,
  textSize,
  textBusy,
  noticeColor,
  noticeBusy,
  onTheme,
  onPalette,
  onTextSize,
  onNoticeColor,
}: IAppearanceSectionProps) => (
  <>
    <Group title={t('next.settings.look.mode')} card="hx-looks" foot={t('next.settings.look.mode.foot')}>
      <div
        className="hx-looks__grid hx-looks__grid--modes"
        role="radiogroup"
        aria-label={t('next.settings.look.mode')}
      >
        {THEMES.map((value) => (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={value === theme}
            className="hx-looks__card"
            onClick={(event) => value !== theme && onTheme(value, originOf(event))}
          >
            <span className="hx-looks__pic" data-split={value === 'system' || undefined}>
              {value === 'dark' ? null : <Miniature mode="light" palette={palette} />}
              {value === 'light' ? null : <Miniature mode="dark" palette={palette} />}
              <span className="hx-looks__check">
                <Icon name="check" size="sm" />
              </span>
            </span>
            <span className="hx-looks__name">
              <Icon name={THEME_ICON[value]} size="sm" />
              {t(`next.settings.general.theme.${value}`)}
            </span>
          </button>
        ))}
      </div>
    </Group>
    <Group title={t('next.settings.look.palette')} card="hx-looks">
      <div className="hx-looks__grid" role="radiogroup" aria-label={t('next.settings.look.palette')}>
        {PALETTES.map((value) => (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={value === palette}
            className="hx-looks__card"
            onClick={(event) => value !== palette && onPalette(value, originOf(event))}
          >
            <span className="hx-looks__pic">
              <Miniature mode={mode} palette={value} />
              <span className="hx-looks__check">
                <Icon name="check" size="sm" />
              </span>
            </span>
            <span className="hx-looks__text">
              <span className="hx-looks__name">{t(`next.settings.look.palette.${value}`)}</span>
              <span className="hx-looks__sub">{t(`next.settings.look.palette.${value}.sub`)}</span>
            </span>
          </button>
        ))}
      </div>
    </Group>
    <Group title={t('next.settings.look.text')}>
      <SegRow
        label={t('next.settings.general.textSize')}
        hint={t('next.settings.general.textSize.hint')}
        value={textSize}
        choices={TEXT_SIZES.map((value) => ({ value, label: t(`next.settings.general.textSize.${value}`) }))}
        busy={textBusy}
        onPick={onTextSize}
      />
    </Group>
    <Group title={t('next.settings.look.notices')}>
      <SegRow
        label={t('next.settings.look.noticeColor')}
        hint={t('next.settings.look.noticeColor.hint')}
        value={noticeColor}
        choices={NOTICE_COLORS.map((value) => ({
          value,
          label: t(`next.settings.look.noticeColor.${value}`),
        }))}
        busy={noticeBusy}
        onPick={onNoticeColor}
      />
    </Group>
  </>
);

export default AppearanceSection;
