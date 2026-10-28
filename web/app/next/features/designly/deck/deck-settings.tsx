// The settings of a new presentation (board: «Настройки»): the length, the slides, how to speak, the prompter text and
// the company style. The length and the slides may be left to the agent. A popover anchored under the composer (over it
// when there is no room below, never on top of the field) on a wide window, a bottom sheet with a scrim on a phone
// (560 px, the board's breakpoint). It opens in 240 ms (380 on the phone) and leaves in 160 ms (240); it closes on the scrim,
// the cross, «Готово», Esc and, on the phone, a swipe down.
import { useEffect, useEffectEvent, useLayoutEffect, useState } from 'react';
import Icon from '@/next/ui/icon';
import Segmented from '@/next/ui/segmented';
import { SheetGrip, useFocusTrap, useSheetDrag } from '@/next/ui/sheet';
import { usePresentFlag } from '@/next/ui/presence';
import Switch from '@/next/ui/switch';
import { useMedia } from '@/next/ui/use-media';
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

/** The board's phone breakpoint, where the popover becomes a sheet. */
export const DECK_PHONE_QUERY = '(max-width: 560px)';
const EXIT_WIDE_MS = 160;
const EXIT_PHONE_MS = 240;

const TONE_KEY = { talk: 'toneTalk', biz: 'toneBiz', short: 'toneShort' } as const;

interface IDeckSettingsProps {
  open: boolean;
  onClose: () => void;
  settings: IDeckCreateSettings;
  onChange: (next: IDeckCreateSettings) => void;
  /** The template's file name; null when none is attached (the style row is then not offered). */
  styleName: string | null;
  /** The composer the popover hangs from; it opens under it, or over it when there is no room below. */
  anchor: HTMLElement | null;
}

/** The gap between the composer and the popover, and the margin it keeps from the window's edge. */
const GAP = 8;
const EDGE = 12;

/** Puts the popover under the anchor (over it when the room below is smaller), through properties set on the panel. */
const place = (panel: HTMLElement, anchor: HTMLElement) => {
  const a = anchor.getBoundingClientRect();
  const below = window.innerHeight - a.bottom - GAP - EDGE;
  const above = a.top - GAP - EDGE;
  const need = panel.scrollHeight;
  const up = need > below && above > below;
  const room = Math.max(160, up ? above : below);
  const width = Math.min(380, a.width);
  const left = Math.min(Math.max(EDGE, a.left), Math.max(EDGE, window.innerWidth - width - EDGE));
  panel.dataset.side = up ? 'top' : 'bottom';
  panel.style.setProperty('--set-left', `${left}px`);
  panel.style.setProperty('--set-w', `${width}px`);
  panel.style.setProperty('--set-max', `${room}px`);
  panel.style.setProperty('--set-y', `${up ? window.innerHeight - a.top + GAP : a.bottom + GAP}px`);
};

const DeckSettings = ({ open, onClose, settings, onChange, styleName, anchor }: IDeckSettingsProps) => {
  const phone = useMedia(DECK_PHONE_QUERY);
  const { mounted, state } = usePresentFlag(open, phone ? EXIT_PHONE_MS : EXIT_WIDE_MS);
  const [box, setBox] = useState<HTMLElement | null>(null);
  const close = useEffectEvent(onClose);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);
  useSheetDrag(box, onClose, open && phone);
  useFocusTrap(box, open);

  useLayoutEffect(() => {
    if (!box || !anchor || phone) return;
    const put = () => place(box, anchor);
    put();
    window.addEventListener('resize', put);
    return () => window.removeEventListener('resize', put);
  }, [box, anchor, phone, mounted]);

  if (!mounted) return null;
  const slides = slidesOf(settings);
  const perSlide = secondsPerSlide(settings);
  const hand = settings.slides !== null;
  const { minutes } = settings;
  return (
    <>
      <div className="hx-deck-scrim" data-state={state} onClick={onClose} aria-hidden="true" />
      <section
        ref={setBox}
        className="hx-deck-set"
        data-state={state}
        role="dialog"
        aria-label={t('next.deck.settings.label')}
        tabIndex={-1}
      >
        <SheetGrip />
        <div className="hx-deck-set__head">
          <b>{t('next.deck.settings.title')}</b>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label={t('next.deck.settings.close')}
            onClick={onClose}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>

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

        <button type="button" className="hx-btn hx-deck-set__go" data-variant="primary" onClick={onClose}>
          {t('next.deck.settings.done')}
        </button>
      </section>
    </>
  );
};

export default DeckSettings;
