// The create screen of a presentation (board scene «Создание»): the brief, files that come along, the settings chip,
// the design system chip (a system of the machine, a company template from a .pptx, or none), the prompter switch,
// «Создать», and the card of a company template when a .pptx is attached. The length and the slide count are optional:
// the settings chip says only what was set. The settings open as a popover under the composer or, on a phone, a
// sheet (deck-settings.tsx). A template is read by the runtime (pptx-style) for its colours,
// fonts and layouts; the file also goes with the brief as an attachment. A brief sent waits for the runtime.
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { useUploads } from '@/next/bindings/use-uploads';
import { readPptxStyle } from '@/next/features/designly/deck/api';
import {
  DEFAULT_CREATE_SETTINGS,
  deckTitleOf,
  isPptx,
  toWireDeck,
  type IDeckCreateSettings,
} from '@/next/features/designly/deck/create-settings';
import DsPicker from '@/next/features/designly/ds-picker';
import type { IDesignSystem } from '@/next/features/designly/api';
import DeckSettings from '@/next/features/designly/deck/deck-settings';
import type { IDeckPptxStyle } from '@/next/features/designly/deck/types';
import type { DesignlyDeck, MachineId } from '@/next/protocol/v2';
import { clearDraft, setDraftText, useDraft, type DesignDraftKey } from '@/next/stores/drafts';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { toast } from '@/next/ui/toast';
import { plural, t } from '@/next/i18n';

/** The draft of a presentation not yet made: the brief and its uploaded files survive a reload. */
export const DECK_NEW_DRAFT: DesignDraftKey = 'design:new-slides';

export interface IDeckCreateInput {
  title: string;
  brief: string;
  deck: DesignlyDeck;
  /** The machine's design system the deck is built on; null for none. */
  designSystem: string | null;
  attachmentIds: string[];
}

interface IDeckCreateProps {
  machine: MachineId;
  /** The design systems the machine has (the same offer as a site's start). */
  systems: readonly IDesignSystem[];
  onBack: () => void;
  /** Sends the brief; resolves once the runtime made the project, rejects when it did not (the screen stays). */
  onCreate: (input: IDeckCreateInput) => Promise<void>;
}

type TStyleRead =
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

/** What the settings chip says: «Настройки» alone while nothing is set, else only what was set. */
const chipLabel = ({ minutes, slides }: IDeckCreateSettings): string => {
  if (minutes !== null && slides !== null) return t('next.deck.create.chipBoth', { m: minutes, s: slides });
  if (minutes !== null) return t('next.deck.create.chipMinutes', { m: minutes });
  if (slides !== null) return plural(slides, 'next.deck.create.chipSlides', { n: slides });
  return t('next.deck.create.settingsChip');
};

const DeckCreate = ({ machine, systems, onBack, onCreate }: IDeckCreateProps) => {
  const draft = useDraft(DECK_NEW_DRAFT);
  const brief = draft?.text ?? '';
  const uploads = useUploads(machine, DECK_NEW_DRAFT, draft?.attachments ?? []);
  const [settings, setSettings] = useState<IDeckCreateSettings>(DEFAULT_CREATE_SETTINGS);
  const [open, setOpen] = useState(false);
  const [ds, setDs] = useState<string | null>(null);
  const [box, setBox] = useState<HTMLElement | null>(null);
  const [reads, setReads] = useState<TStyleRead[]>([]);
  const [peek, setPeek] = useState(false);
  const [sending, setSending] = useState(false);
  const field = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const templatePicker = useRef<HTMLInputElement>(null);

  const style = reads.find((r) => r.state === 'ok')?.style ?? null;
  const reading = reads.some((r) => r.state === 'reading');
  const styled = style !== null && settings.useStyle;

  // A look comes from one place: a design system or the company template, the last one chosen.
  const chooseSystem = (id: string | null) => {
    setDs(id);
    setSettings((now) => ({ ...now, useStyle: false }));
  };
  const chooseTemplate = () => {
    if (style) {
      setDs(null);
      setSettings((now) => ({ ...now, useStyle: true }));
    } else templatePicker.current?.click();
  };
  const changeSettings = (next: IDeckCreateSettings) => {
    if (next.useStyle && !settings.useStyle) setDs(null);
    setSettings(next);
  };

  const addFiles = (files: File[]) => {
    uploads.onFiles(files);
    if (files.some((f) => isPptx(f.name))) {
      setDs(null);
      setSettings((now) => ({ ...now, useStyle: true }));
    }
    for (const file of files.filter((f) => isPptx(f.name))) {
      setReads((rows) => [
        ...rows.filter((r) => r.name !== file.name),
        { name: file.name, state: 'reading' },
      ]);
      void readPptxStyle(machine, file).then(
        (read) => {
          if (!read) toast(t('next.deck.create.styleUnreadable', { name: file.name }), { tone: 'error' });
          setReads((rows) =>
            rows.map((r) =>
              r.name === file.name
                ? read
                  ? { name: r.name, state: 'ok', style: read }
                  : { name: r.name, state: 'failed' }
                : r,
            ),
          );
        },
        (failure: Error) => {
          toast(t('next.deck.create.styleUnreadable', { name: file.name }), {
            tone: 'error',
            description: failure.message,
          });
          setReads((rows) => rows.map((r) => (r.name === file.name ? { name: r.name, state: 'failed' } : r)));
        },
      );
    }
  };

  const removeFile = (id: string, name: string) => {
    uploads.onRemove(id);
    setReads((rows) => rows.filter((r) => r.name !== name));
  };

  const send = () => {
    const text = brief.trim();
    if (sending) return;
    // An empty field is not an error: the button takes the person back to it, as Designly's start does.
    if (!text) {
      field.current?.focus();
      return;
    }
    setSending(true);
    void onCreate({
      title: deckTitleOf(text),
      brief: text,
      deck: toWireDeck(settings, style),
      designSystem: ds,
      attachmentIds: uploads.ready.map((a) => a.id),
    })
      .then(() => {
        clearDraft(DECK_NEW_DRAFT);
        uploads.reset();
      })
      // A refusal is said by the caller; the brief stays in the field to send again.
      .catch(() => undefined)
      .finally(() => setSending(false));
  };
  const onKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      send();
    }
  };

  const busy = sending || uploads.uploading || reading;
  return (
    <section className="hx-deck-new" data-screen="create">
      <header className="hx-deck-new__bar">
        <button
          type="button"
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          data-shape="icon"
          aria-label={t('next.deck.bar.back')}
          onClick={onBack}
        >
          <Icon name="arrow-left" />
        </button>
        <div className="hx-deck-new__addr">
          <b>{t('next.tabs.designly')}</b>
          <span>/</span>
          <em>{t('next.deck.create.title')}</em>
        </div>
      </header>

      <div className="hx-deck-new__stage">
        <h2>{t('next.deck.create.title')}</h2>
        <div className="hx-deck-box" ref={setBox}>
          <textarea
            ref={field}
            aria-label={t('next.deck.create.brief')}
            placeholder={t('next.deck.create.placeholder')}
            value={brief}
            disabled={sending}
            onChange={(event) => setDraftText(DECK_NEW_DRAFT, event.target.value)}
            onKeyDown={onKey}
          />
          {uploads.attachments.length > 0 && (
            <div className="hx-deck-files">
              {uploads.attachments.map((a) => {
                const template = isPptx(a.name);
                const read = reads.find((r) => r.name === a.name);
                return (
                  <span
                    key={a.id}
                    className="hx-deck-file"
                    data-state={a.status}
                    title={a.error ?? undefined}
                  >
                    <Icon
                      name={a.status === 'uploading' ? 'loader-circle' : template ? 'layers' : 'file-text'}
                      size="sm"
                    />
                    {a.name}
                    {template && read?.state === 'ok' && <small>{t('next.deck.create.styleChip')}</small>}
                    {a.status === 'error' && (
                      <button
                        type="button"
                        aria-label={t('next.composer.retryAttachment', { name: a.name })}
                        onClick={() => uploads.onRetry(a.id)}
                      >
                        <Icon name="rotate-ccw" size="sm" />
                      </button>
                    )}
                    <button
                      type="button"
                      aria-label={t('next.composer.removeAttachment', { name: a.name })}
                      onClick={() => removeFile(a.id, a.name)}
                    >
                      <Icon name="x" size="sm" />
                    </button>
                  </span>
                );
              })}
            </div>
          )}
          <div className="hx-deck-boxrow">
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              tabIndex={-1}
              onChange={(event) => {
                addFiles([...(event.target.files ?? [])]);
                event.target.value = '';
              }}
            />
            <input
              ref={templatePicker}
              type="file"
              accept=".pptx"
              hidden
              tabIndex={-1}
              onChange={(event) => {
                addFiles([...(event.target.files ?? [])]);
                event.target.value = '';
              }}
            />
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              data-shape="icon"
              aria-label={t('next.deck.create.attach')}
              onClick={() => picker.current?.click()}
            >
              <Icon name="paperclip" />
            </button>
            <DsPicker
              systems={systems}
              value={ds}
              shown
              variant="chip"
              emptyLabel={t('next.deck.create.dsChip')}
              onChange={chooseSystem}
              extra={{
                label: t('next.deck.create.dsTemplate'),
                desc: t('next.deck.create.dsTemplateDesc'),
                active: styled,
                name: style?.name ?? '',
                onPick: chooseTemplate,
              }}
            />
            <button
              type="button"
              className="hx-deck-chip"
              data-on={open ? '1' : '0'}
              aria-expanded={open}
              aria-haspopup="dialog"
              onClick={() => setOpen(true)}
            >
              <Icon name="sliders-horizontal" size="sm" />
              {chipLabel(settings)}
            </button>
            <button
              type="button"
              className="hx-deck-chip"
              data-desk="true"
              data-on={settings.script ? '1' : '0'}
              aria-pressed={settings.script}
              onClick={() => setSettings({ ...settings, script: !settings.script })}
            >
              {t('next.deck.create.script')}
            </button>
            <span className="hx-deck-boxrow__grow" />
            <button
              type="button"
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              data-busy={busy || undefined}
              aria-busy={busy || undefined}
              disabled={busy}
              onClick={send}
            >
              {t('next.deck.create.submit')}
            </button>
          </div>
        </div>

        {styled && style && (
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
        )}
      </div>

      <DeckSettings
        open={open}
        onClose={() => setOpen(false)}
        settings={settings}
        onChange={changeSettings}
        styleName={style?.name ?? null}
        anchor={box}
      />
    </section>
  );
};

export default DeckCreate;
