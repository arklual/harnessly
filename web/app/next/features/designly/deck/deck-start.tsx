// What a presentation adds to Designly's start: its settings, the company template read from an attached .pptx
// (pptx-style in the runtime: colours, fonts, layouts; the file also goes with the brief), the card that says a
// template was found, and what the field's «Настройки» button says. A look comes from one place: a design system or
// the template, the last one chosen.
import { useEffect, useRef, useState, type RefObject } from 'react';
import type { IUploads } from '@/next/bindings/use-uploads';
import { readPptxStyle } from '@/next/features/designly/deck/api';
import {
  DEFAULT_CREATE_SETTINGS,
  isPptx,
  toWireDeck,
  type IDeckCreateSettings,
} from '@/next/features/designly/deck/create-settings';
import type { IDeckPptxStyle } from '@/next/features/designly/deck/types';
import type { DesignlyDeck, MachineId } from '@/next/protocol/v2';
import Popover from '@/next/ui/popover';
import { toast } from '@/next/ui/toast';
import { plural, t } from '@/next/i18n';

export type TStyleRead =
  | { name: string; state: 'reading' }
  | { name: string; state: 'failed' }
  | { name: string; state: 'ok'; style: IDeckPptxStyle };

/** A colour dot: the colour goes in through a property set on the element, not an inline style attribute. */
const Dot = ({ color }: { color: string }) => {
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    ref.current?.style.setProperty('--c', color);
  }, [color]);
  return <i ref={ref} className="hx-deck-found__dot" />;
};

/** The look of a template as a small slide: its variables are set on the element, the fonts named under it. */
const StylePreview = ({ style }: { style: IDeckPptxStyle }) => {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    for (const [name, value] of Object.entries(style.vars)) el.style.setProperty(name, value);
    return () => {
      for (const name of Object.keys(style.vars)) el.style.removeProperty(name);
    };
  }, [style]);
  return (
    <div className="hx-deck-prev">
      <div className="hx-deck-prev__slide" ref={ref}>
        <b>{t('next.deck.create.previewHead')}</b>
        <span>{t('next.deck.create.previewBody')}</span>
        <i />
      </div>
      <div className="hx-deck-prev__meta">
        <div className="hx-deck-prev__dots">
          {style.colors.map((c, i) => (
            <Dot key={`${i}-${c}`} color={c} />
          ))}
        </div>
        <span>{t('next.deck.create.previewFonts', { head: style.fonts.head, body: style.fonts.body })}</span>
      </div>
    </div>
  );
};

/** Under the field: a template was found in the attached file, with its picture behind «Посмотреть». */
export const DeckFound = ({ style }: { style: IDeckPptxStyle }) => {
  const [peek, setPeek] = useState(false);
  return (
    <div className="hx-deck-found">
      <span className="hx-deck-found__dots">
        {style.colors.slice(0, 3).map((c, i) => (
          <Dot key={`${i}-${c}`} color={c} />
        ))}
      </span>
      <div>
        <b>{t('next.deck.create.styleTitle')}</b>
        {plural(style.layouts, 'next.deck.create.styleBody', { n: style.layouts })}
      </div>
      <Popover
        trigger={
          <button type="button" className="hx-btn" data-variant="ghost" data-size="sm">
            {t('next.deck.create.styleView')}
          </button>
        }
        open={peek}
        onOpenChange={setPeek}
        label={t('next.deck.create.styleTitle')}
        className="hx-deck-peek"
        side="bottom"
        align="end"
      >
        <StylePreview style={style} />
      </Popover>
    </div>
  );
};

const TONE_KEY = { talk: 'toneTalk', biz: 'toneBiz', short: 'toneShort' } as const;

/** What the settings button says: «Настройки» alone while all is as it starts, else what differs, joined by « · ». */
export const deckSettingsLabel = (s: IDeckCreateSettings, look: string | null): string => {
  const parts: string[] = [];
  if (look !== null) parts.push(look);
  if (s.minutes !== null && s.slides !== null)
    parts.push(t('next.deck.create.chipBoth', { m: s.minutes, s: s.slides }));
  else if (s.minutes !== null) parts.push(t('next.deck.create.chipMinutes', { m: s.minutes }));
  else if (s.slides !== null) parts.push(plural(s.slides, 'next.deck.create.chipSlides', { n: s.slides }));
  if (s.tone !== DEFAULT_CREATE_SETTINGS.tone) parts.push(t(`next.deck.settings.${TONE_KEY[s.tone]}`));
  if (s.script) parts.push(t('next.deck.create.scriptShort'));
  return parts.length > 0 ? parts.join(' · ') : t('next.deck.create.settingsChip');
};

interface IDeckStartOptions {
  machine: MachineId;
  uploads: IUploads;
  /** The design system picked on the start, shared by every kind; null for none. */
  ds: string | null;
  onDs: (id: string | null) => void;
  /** The start is making a presentation now: only then does an attached template take over the look. */
  active: boolean;
  /** The file field «Шаблон компании» opens while no template is attached. */
  templatePicker: RefObject<HTMLInputElement | null>;
}

export const useDeckStart = ({ machine, uploads, ds, onDs, active, templatePicker }: IDeckStartOptions) => {
  const [settings, setSettings] = useState<IDeckCreateSettings>(DEFAULT_CREATE_SETTINGS);
  const [reads, setReads] = useState<TStyleRead[]>([]);

  const style = reads.find((r) => r.state === 'ok')?.style ?? null;
  const reading = reads.some((r) => r.state === 'reading');
  const styled = style !== null && settings.useStyle && ds === null;

  const chooseSystem = (id: string | null) => {
    onDs(id);
    setSettings((now) => ({ ...now, useStyle: false }));
  };
  const chooseTemplate = () => {
    if (style) {
      onDs(null);
      setSettings((now) => ({ ...now, useStyle: true }));
    } else templatePicker.current?.click();
  };
  const changeSettings = (next: IDeckCreateSettings) => {
    if (next.useStyle && !settings.useStyle) onDs(null);
    setSettings(next);
  };

  const addFiles = (files: File[]) => {
    uploads.onFiles(files);
    const templates = files.filter((f) => isPptx(f.name));
    if (templates.length > 0 && active) {
      onDs(null);
      setSettings((now) => ({ ...now, useStyle: true }));
    }
    for (const file of templates) {
      setReads((rows) => [
        ...rows.filter((r) => r.name !== file.name),
        { name: file.name, state: 'reading' },
      ]);
      const settle = (read: IDeckPptxStyle | null) =>
        setReads((rows) =>
          rows.map((r) => {
            if (r.name !== file.name) return r;
            return read ? { name: r.name, state: 'ok', style: read } : { name: r.name, state: 'failed' };
          }),
        );
      void readPptxStyle(machine, file).then(
        (read) => {
          if (!read) toast(t('next.deck.create.styleUnreadable', { name: file.name }), { tone: 'error' });
          settle(read);
        },
        (failure: Error) => {
          toast(t('next.deck.create.styleUnreadable', { name: file.name }), {
            tone: 'error',
            description: failure.message,
          });
          settle(null);
        },
      );
    }
  };

  const removeFile = (id: string, name: string) => {
    uploads.onRemove(id);
    setReads((rows) => rows.filter((r) => r.name !== name));
  };

  const wire = (): DesignlyDeck => toWireDeck({ ...settings, useStyle: styled }, style);

  return {
    settings,
    reads,
    style,
    reading,
    styled,
    chooseSystem,
    chooseTemplate,
    changeSettings,
    addFiles,
    removeFile,
    wire,
    /** After a project was made: the template belonged to the brief that went. */
    reset: () => {
      setReads([]);
      setSettings(DEFAULT_CREATE_SETTINGS);
    },
  };
};
