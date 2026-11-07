// The settings of a new presentation (board: «Настройки»): the design system or the company template, the length, the
// slides, how to speak, the prompter text and whether the template's style is used. The length and the slides may be
// left to the agent. The panel itself (popover or sheet) is create-panel.tsx.
import CreatePanel, { DsChoice, type IDsOther, type IKitAdd } from '@/next/features/designly/create-panel';
import type { IDesignSystem } from '@/next/features/designly/api';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import Switch from '@/next/ui/switch';
import {
  DECK_DURATIONS,
  DECK_TONES,
  secondsPerSlide,
  slidesOf,
  stepSlides,
  wordsOf,
  type IDeckCreateSettings,
} from '@/next/features/designly/deck/create-settings';
import { plural, t } from '@/next/i18n';

const TONE_KEY = { talk: 'toneTalk', biz: 'toneBiz', short: 'toneShort' } as const;

interface IDeckSettingsProps {
  open: boolean;
  onClose: () => void;
  settings: IDeckCreateSettings;
  onChange: (next: IDeckCreateSettings) => void;
  /** The template's file name; null when none is attached (the style row is then not offered). */
  styleName: string | null;
  /** The field the popover hangs from. */
  anchor: HTMLElement | null;
  systems: readonly IDesignSystem[];
  /** The machine's design system picked; null for none (or the template). */
  ds: string | null;
  onDs: (id: string | null) => void;
  template: IDsOther;
  kit?: IKitAdd;
}

const DeckSettings = ({
  open,
  onClose,
  settings,
  onChange,
  styleName,
  anchor,
  systems,
  ds,
  onDs,
  template,
  kit,
}: IDeckSettingsProps) => {
  const slides = slidesOf(settings);
  const perSlide = secondsPerSlide(settings);
  const hand = settings.slides !== null;
  const { minutes } = settings;
  return (
    <CreatePanel open={open} onClose={onClose} anchor={anchor} label={t('next.deck.settings.label')}>
      <DsChoice systems={systems} value={ds} onChange={onDs} other={template} kit={kit} />
      <div className="hx-deck-set__label">{t('next.deck.settings.length')}</div>
      <div className="hx-deck-set__big">
        {minutes === null ? (
          <b>{t('next.deck.settings.any')}</b>
        ) : (
          <>
            <b>{minutes}</b> {t('next.deck.settings.minutes')}
            <span>{plural(wordsOf(minutes), 'next.deck.settings.words', { n: wordsOf(minutes) })}</span>
          </>
        )}
      </div>
      <div className="hx-deck-set__steps" role="radiogroup" aria-label={t('next.deck.settings.length')}>
        <button
          type="button"
          role="radio"
          aria-checked={minutes === null}
          data-on={minutes === null ? '1' : '0'}
          onClick={() => onChange({ ...settings, minutes: null })}
        >
          {t('next.deck.settings.any')}
        </button>
        {DECK_DURATIONS.map((m) => (
          <button
            key={m}
            type="button"
            role="radio"
            aria-checked={m === minutes}
            data-on={m === minutes ? '1' : '0'}
            onClick={() => onChange({ ...settings, minutes: m })}
          >
            {m}
          </button>
        ))}
      </div>

      <div className="hx-deck-set__label">{t('next.deck.settings.slides')}</div>
      <div className="hx-deck-set__row">
        <span className="hx-deck-set__v">
          {hand ? t('next.deck.settings.manual') : t('next.deck.settings.auto')}
          {perSlide !== null && <small>{t('next.deck.settings.perSlide', { s: perSlide })}</small>}
          {!hand && perSlide === null && <small>{t('next.deck.settings.agentDecides')}</small>}
          {hand && (
            <button
              type="button"
              className="hx-deck-set__reset"
              onClick={() => onChange({ ...settings, slides: null })}
            >
              {t('next.deck.settings.autoReset')}
            </button>
          )}
        </span>
        <div className="hx-deck-set__stepper">
          <button
            type="button"
            aria-label={t('next.deck.settings.fewer')}
            disabled={slides !== null && slides <= 2}
            onClick={() => onChange(stepSlides(settings, -1))}
          >
            <Icon name="minus" size="sm" />
          </button>
          <b>{slides ?? '—'}</b>
          <button
            type="button"
            aria-label={t('next.deck.settings.more')}
            disabled={slides !== null && slides >= 40}
            onClick={() => onChange(stepSlides(settings, 1))}
          >
            <Icon name="plus" size="sm" />
          </button>
        </div>
      </div>

      <div className="hx-deck-set__label">{t('next.deck.settings.tone')}</div>
      <Segmented
        label={t('next.deck.settings.tone')}
        value={settings.tone}
        options={DECK_TONES.map((tone) => ({
          value: tone,
          label: t(`next.deck.settings.${TONE_KEY[tone]}`),
        }))}
        onChange={(tone) => onChange({ ...settings, tone })}
      />

      <div className="hx-deck-set__tog">
        <span>
          {t('next.deck.settings.script')}
          <small>{t('next.deck.settings.scriptHint')}</small>
        </span>
        <Switch
          label={t('next.deck.settings.script')}
          checked={settings.script}
          onChange={(script) => onChange({ ...settings, script })}
        />
      </div>
      {styleName !== null && (
        <div className="hx-deck-set__tog">
          <span>
            {t('next.deck.settings.style')}
            <small>{styleName}</small>
          </span>
          <Switch
            label={t('next.deck.settings.style')}
            checked={settings.useStyle}
            onChange={(useStyle) => onChange({ ...settings, useStyle })}
          />
        </div>
      )}
    </CreatePanel>
  );
};

export default DeckSettings;
