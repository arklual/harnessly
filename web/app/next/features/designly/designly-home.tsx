// Designly's start (the design's .hx-dl): «Что нарисуем?» with the one field on top, what to make under it, and the
// library below — the machine's designs as cards with their pictures. A tile switches the kind in place: the field,
// its text and its files stay, «Настройки» offers what the kind has. Scrolled, the field goes up and the library's row
// sticks. A card opens the design; a brief sent starts a project and its agent, and its card draws while it works.
import { TOUR_ORDER } from '@/next/features/tour/tracks';
import { startTour, tourFactGroup } from '@/next/stores/tour';
import { registerLibraryControl } from '@/next/stores/library-view';
import {
  memo,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ClipboardEvent,
  type KeyboardEvent,
  type RefObject,
} from 'react';
import { Menu } from '@base-ui/react/menu';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import LibraryBar from '@/next/features/designly/library-bar';
import {
  Crumbs,
  FolderDeleteDialog,
  FolderNameDialog,
  FolderTiles,
  MoveDialog,
  type IFolderForm,
} from '@/next/features/designly/library-folders';
import TrashView from '@/next/features/designly/library-trash';
import type { ILibraryOrganizer } from '@/next/features/designly/library-organizer';
import {
  DEMO_IDS,
  NO_FILTERS,
  childrenOf,
  isNarrowed,
  matchesFilters,
  projectsIn,
  routeToCard,
  sortCards,
  systemsOf,
  type ILibraryFilters,
} from '@/next/features/designly/library-model';
import { useCardDrag } from '@/next/features/designly/use-card-drag';
import type { IFolder } from '@/next/features/designly/api';
import { usePresence, usePresentFlag } from '@/next/ui/presence';
import AttachmentThumb from '@/next/ui/attachment-thumb';
import { useFileDrop } from '@/next/features/composer/use-file-drop';
import Icon from '@/next/ui/icon';
import CreatePanel, { DsChoice } from '@/next/features/designly/create-panel';
import { DeckFound, deckSettingsLabel, useDeckStart } from '@/next/features/designly/deck/deck-start';
import DeckSettings from '@/next/features/designly/deck/deck-settings';
import { isPptx } from '@/next/features/designly/deck/create-settings';
import VideoSettings, { videoSettingsLabel } from '@/next/features/designly/video/video-settings';
import {
  DEFAULT_VIDEO_SETTINGS,
  toWireVideo,
  type IVideoCreateSettings,
} from '@/next/features/designly/video/video-create-settings';
import { RecordingTime, RecordingWave } from '@/next/features/composer/composer';
import ModelPicker, { type IModelPickerProps } from '@/next/features/composer/model-picker';
import type { Recording } from '@/next/features/composer/types';
import type { IUploads } from '@/next/bindings/use-uploads';
import { RoundButton } from '@/next/ui/button';
import { useFieldEdges, useFitField } from '@/next/ui/use-fit-field';
import { useStableFn } from '@/next/ui/use-stable-fn';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import Swap from '@/next/ui/swap';
import { HSCROLL_DIR, useHScroll } from '@/next/ui/use-h-scroll';
import type { IDesignSystem } from '@/next/features/designly/api';
import KitShelf, { type IKitShelf } from '@/next/features/designly/kit/kit-shelf';
import {
  DESIGN_SCOPE,
  DESIGN_SCOPES,
  inScope,
  initialOf,
  type TDesignScope,
} from '@/next/features/designly/owners';
import type { IAppKind } from '@/next/features/app-catalog/api';
import type { DesignlyDeck, DesignlyVideo, DesignOwner, MachineId } from '@/next/protocol/v2';
import { plural, t } from '@/next/i18n';
import { formatAgoFull } from '@/next/i18n/format';
import '@/next/styles/designly.css';

export const DESIGN_KIND = { site: 'site', slides: 'slides', video: 'video', ds: 'ds' } as const;
export type TDesignKind = (typeof DESIGN_KIND)[keyof typeof DESIGN_KIND];

const KIND_ICON: Record<TDesignKind, string> = {
  [DESIGN_KIND.site]: 'app-window',
  [DESIGN_KIND.slides]: 'presentation',
  [DESIGN_KIND.video]: 'clapperboard',
  [DESIGN_KIND.ds]: 'palette',
};
const KINDS = [DESIGN_KIND.site, DESIGN_KIND.slides, DESIGN_KIND.video, DESIGN_KIND.ds] as const;

export interface IDesignCard {
  id: string;
  title: string;
  boards: number;
  updatedAt: number;
  /** The picture of its main board; null while there is none. */
  image: string | null;
  kind: TDesignKind;
  pinned: boolean;
  /** An agent is drawing it right now. */
  drawing: boolean;
  /** Who made it; null for a project older than owners. */
  owner: DesignOwner | null;
  /** The person may edit it, so may delete it. */
  canDelete?: boolean;
  /** The person's folder it lies in; null or absent at the top. */
  folder?: string | null;
  /** The design system or kit it was built on, when the project recorded one. */
  system?: { id: string; label: string } | null;
  /** For a document of a type an installed app adds: the type's name, shown where a design shows its kind. */
  appKind?: string;
}

/** What the field sends: the brief with what its kind adds. */
export interface IDesignStart {
  brief: string;
  kind: TDesignKind;
  /** The machine's design system it is built on; null for none. */
  designSystem: string | null;
  deck: DesignlyDeck | null;
  video: DesignlyVideo | null;
  attachmentIds: string[];
}

/** The start of a project: the brief's field, the kinds, the model and the settings of each kind. */
export interface IDesignlyCompose {
  machine: MachineId;
  /** The kind the field opens on; an address of its own can ask for one. */
  initialKind?: TDesignKind;
  /** The design systems a new project can take; null while they load. */
  systems: readonly IDesignSystem[] | null;
  /** A kit was added: the systems are read again. Without it the list has no «Добавить дизайн-систему». */
  onKitAdded?: () => void;
  /** Sending a brief: resolves once the project and its agent exist; rejects (the caller says why) when they do not. */
  onCreate: (start: IDesignStart) => Promise<void>;
  /** The brief in the field; the screen keeps it so dictation can write into it. */
  brief: string;
  onBrief: (value: string) => void;
  /** The files going with the brief. */
  uploads: IUploads;
  /** The agent, model and depth that draw, as in the chat; null while the machine's agents are unknown. */
  picker: IModelPickerProps | null;
  /** Dictation into the brief; without onMic the microphone is not offered. */
  voice: IDesignlyVoice;
  /** The document types installed apps add; a tile each. null while they load or when the machine lists none. */
  appKinds?: readonly IAppKind[] | null;
  /** A new document of an app's type, named by the brief typed so far; resolves once it is made, rejects (the caller says why). */
  onAppKind?: (kind: IAppKind, brief: string) => Promise<void>;
}

export interface IDesignlyHomeProps {
  /** null while the library loads; an error string when it did not. */
  cards: readonly IDesignCard[] | null;
  error: string | null;
  now: number;
  /** Pins or unpins a card; resolves once the machine kept it. */
  onPin: (id: string, pinned: boolean) => Promise<void>;
  /** Moves a project to the trash; resolves once the machine did, rejects (the caller says why) when it did not. */
  onDelete?: (id: string) => Promise<void>;
  /** Folders, moving and the trash; absent where the machine cannot lay the library out. */
  organizer?: ILibraryOrganizer;
  /** Copies back the demo projects the library no longer has; resolves once the list has them. */
  onRestoreDemo?: () => Promise<void>;
  onOpen: (id: string) => void;
  onRetry: () => void;
  /** Where a project is started; null is the library alone (the right pane's: its chat is the field). */
  compose: IDesignlyCompose | null;
  /** What the machine lacks for designs, with the command that fixes it; null when nothing is missing. */
  /** The installed kits, shown above the library's root; absent where kits are not offered. */
  kits?: IKitShelf;
}

export interface IDesignlyVoice {
  recording: Recording | null;
  onMic?: () => void;
  onCancel: () => void;
  onFinish: () => void;
}

/** The recording bar's exit, as long as hx-recbar-out in composer.css. */
const REC_EXIT_MS = 180;

/** The cards move to their new places over this long, as in the design (dlFlip). */
const FLIP_MS = 620;
const FLIP_EASE = 'cubic-bezier(.2, .8, .2, 1)';
/** A pin's rects are dropped this long after the machine answered: a reorder later is not this pin's. */
const FLIP_KEEP_MS = 1000;

/** The address of a picture asked again: a new query keeps the browser from answering with the failure it remembers. */
const askedAgain = (image: string, round: number) =>
  round === 0 ? image : `${image}${image.includes('?') ? '&' : '?'}r=${round}`;

/** The card's picture through a custom property set on the element, not an inline style attribute. A picture that
 *  does not come leaves a «Повторить» on its place, which asks it again. */
const CardShot = ({ image }: { image: string | null }) => {
  const ref = useRef<HTMLSpanElement>(null);
  const [round, setRound] = useState(0);
  const [broken, setBroken] = useState<string | null>(null);
  const src = image ? askedAgain(image, round) : null;
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (src) el.style.setProperty('--img', `url("${src}")`);
    else el.style.removeProperty('--img');
  }, [src]);
  // The picture is a background, which has no error of its own: the same address is asked for as an image.
  useEffect(() => {
    if (!src) return;
    const probe = new Image();
    probe.onerror = () => setBroken(src);
    probe.src = src;
    return () => {
      probe.onerror = null;
    };
  }, [src]);
  return (
    <>
      <span ref={ref} className="hx-dl-card__img" />
      {src !== null && broken === src && (
        <button
          type="button"
          className="hx-dl-card__retry"
          aria-label={t('next.load.picture')}
          title={t('next.load.picture')}
          onClick={(event) => {
            event.stopPropagation();
            setBroken(null);
            setRound((n) => n + 1);
          }}
        >
          <Icon name="rotate-ccw" size="sm" />
        </button>
      )}
    </>
  );
};

const demoMissing = (cards: readonly IDesignCard[]) => DEMO_IDS.some((id) => !cards.some((c) => c.id === id));

/** What an empty list says: nothing matched the search, else nothing is in the tab. */
const emptyKey = (scope: TDesignScope, searching: boolean) => {
  if (searching) return 'next.designly.notFound';
  if (scope === DESIGN_SCOPE.mine) return 'next.designly.emptyMine';
  if (scope === DESIGN_SCOPE.shared) return 'next.designly.emptyShared';
  return 'next.designly.empty';
};

/** Six cards of the grid's size, so the cards take their places without the page moving. */
const LibrarySkeleton = () => (
  <div className="hx-dl__grid hx-dl__grid--sk" role="status" aria-label={t('next.designly.loading')}>
    {[0, 1, 2, 3, 4, 5].map((i) => (
      <div key={i} className="hx-dl-sk" data-i={i}>
        <span className="hx-sk hx-dl-sk__shot" />
        <span className="hx-sk hx-dl-sk__title" />
        <span className="hx-sk hx-dl-sk__sub" />
      </div>
    ))}
  </div>
);

interface IHeroProps extends IDesignlyCompose {
  /** The field takes focus when the library's «Новый» scrolls back to it. */
  inputRef: RefObject<HTMLTextAreaElement | null>;
}

/** «Что нарисуем?»: the one field with its files, settings, model and microphone, and the kinds under it. */
const Hero = ({
  machine,
  initialKind = DESIGN_KIND.site,
  systems,
  onKitAdded,
  onCreate,
  brief,
  onBrief,
  uploads,
  picker,
  voice,
  appKinds = null,
  onAppKind,
  inputRef,
}: IHeroProps) => {
  const [kind, setKind] = useState<TDesignKind>(initialKind);
  // The app type being made: one at a time, the tile waits for the machine's answer.
  const [making, setMaking] = useState<string | null>(null);
  const [ds, setDs] = useState<string | null>(null);
  const [videoSettings, setVideoSettings] = useState<IVideoCreateSettings>(DEFAULT_VIDEO_SETTINGS);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [box, setBox] = useState<HTMLElement | null>(null);
  const { value: bar, state: barState } = usePresence(voice.recording, REC_EXIT_MS);
  const [sending, setSending] = useState(false);
  const startsRef = useRef<HTMLDivElement>(null);
  const filePicker = useRef<HTMLInputElement>(null);
  const hsStep = useHScroll(startsRef);
  const templatePicker = useRef<HTMLInputElement>(null);
  const deck = useDeckStart({
    machine,
    uploads,
    ds,
    onDs: setDs,
    active: kind === DESIGN_KIND.slides,
    templatePicker,
  });
  useFitField(inputRef, brief);
  useFieldEdges(inputRef, brief);

  const offered = systems ?? [];
  const kit = onKitAdded ? { machine, onAdded: onKitAdded } : undefined;
  const system = offered.find((x) => x.id === ds)?.label ?? null;
  const isDeck = kind === DESIGN_KIND.slides;
  const isVideo = kind === DESIGN_KIND.video;
  const hasSettings = kind !== DESIGN_KIND.ds;
  const busy = sending || uploads.uploading || (isDeck && deck.reading);

  const settingsLabel = () => {
    if (isDeck)
      return deckSettingsLabel(deck.settings, system ?? (deck.styled ? deck.style?.name : null) ?? null);
    if (isVideo) return videoSettingsLabel(videoSettings, system);
    return system ?? t('next.deck.create.settingsChip');
  };
  const label = settingsLabel();

  const send = () => {
    const text = brief.trim();
    if (busy) return;
    // An empty field is not an error: the button takes the person back to it, as the design does.
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    setSending(true);
    void onCreate({
      brief: text,
      kind,
      designSystem: hasSettings ? ds : null,
      deck: isDeck ? deck.wire() : null,
      video: isVideo ? toWireVideo(videoSettings) : null,
      attachmentIds: uploads.ready.map((a) => a.id),
    })
      .then(() => deck.reset())
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
  const pickFiles = (event: ChangeEvent<HTMLInputElement>) => {
    deck.addFiles([...(event.target.files ?? [])]);
    event.target.value = '';
  };
  const dropping = useFileDrop(deck.addFiles);
  const onPaste = (event: ClipboardEvent<HTMLTextAreaElement>) => {
    const files = [...event.clipboardData.files];
    if (files.length === 0) return;
    event.preventDefault();
    deck.addFiles(files);
  };

  return (
    <div className="hx-dl__hero">
      <div className="hx-dl__title">{t('next.designly.title')}</div>
      <div className="hx-dl__sub">{t('next.designly.sub')}</div>
      <div className="hx-dl__composer" ref={setBox} data-drop={dropping || undefined}>
        {dropping && (
          <div className="hx-drop" aria-hidden="true">
            <Icon name="upload" />
            <span>{t('next.composer.dropHere')}</span>
          </div>
        )}
        <textarea
          ref={inputRef}
          className="hx-dl__input"
          rows={2}
          value={brief}
          aria-label={t('next.designly.brief')}
          placeholder={t(`next.designly.hint.${kind}`)}
          disabled={sending}
          onChange={(event) => onBrief(event.target.value)}
          onKeyDown={onKey}
          onPaste={onPaste}
        />
        {uploads.attachments.length > 0 && (
          <div className="hx-deck-files">
            {uploads.attachments.map((a) => {
              const template = isDeck && isPptx(a.name);
              const read = deck.reads.find((r) => r.name === a.name);
              return (
                <span key={a.id} className="hx-deck-file" data-state={a.status} title={a.error ?? undefined}>
                  {a.kind === 'image' && a.status === 'ready' ? (
                    <AttachmentThumb item={a} className="hx-deck-file__thumb" />
                  ) : (
                    <Icon
                      name={a.status === 'uploading' ? 'loader-circle' : template ? 'layers' : 'file-text'}
                      size="sm"
                    />
                  )}
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
                    onClick={() => deck.removeFile(a.id, a.name)}
                  >
                    <Icon name="x" size="sm" />
                  </button>
                </span>
              );
            })}
          </div>
        )}
        {bar && (
          <div className="hx-dl__bar hx-recbar" data-state={barState}>
            <RoundButton
              icon="x"
              label={t('next.composer.cancelRecording')}
              variant="ghost"
              size="sm"
              onClick={voice.onCancel}
            />
            <span className="hx-recbar__live">
              <span className="hx-recbar__dot" />
              {bar.busy ? (
                <span className="hx-recbar__time">{t('next.voice.recognizing')}</span>
              ) : (
                <RecordingTime startedAt={bar.startedAt} />
              )}
            </span>
            <RecordingWave level={bar.level} listening={!bar.busy} />
            <RoundButton
              icon="square"
              iconSize="sm"
              label={t('next.composer.finishRecording')}
              className="hx-recbar__stop"
              busy={bar.busy}
              disabled={bar.busy}
              onClick={voice.onFinish}
            />
          </div>
        )}
        <div className="hx-dl__bar" hidden={bar !== null}>
          <input ref={filePicker} type="file" multiple hidden tabIndex={-1} onChange={pickFiles} />
          <input ref={templatePicker} type="file" accept=".pptx" hidden tabIndex={-1} onChange={pickFiles} />
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            data-shape="icon"
            aria-label={t('next.designly.attach')}
            onClick={() => filePicker.current?.click()}
          >
            <Icon name="paperclip" />
          </button>
          {hasSettings && (
            <button
              type="button"
              className="hx-deck-chip"
              data-on={settingsOpen || label !== t('next.deck.create.settingsChip') ? '1' : '0'}
              aria-expanded={settingsOpen}
              aria-haspopup="dialog"
              onClick={() => setSettingsOpen(true)}
            >
              <Icon name="sliders-horizontal" size="sm" />
              <span className="hx-deck-chip__text">{label}</span>
            </button>
          )}
          <span className="hx-spacer" />
          {picker && (
            <div className="hx-dl__model">
              <ModelPicker {...picker} />
            </div>
          )}
          {voice.onMic && <RoundButton icon="mic" label={t('next.composer.voice')} onClick={voice.onMic} />}
          <RoundButton
            icon="arrow-up"
            variant="accent"
            label={t('next.designly.send')}
            keys={['↵']}
            busy={busy}
            disabled={busy}
            onClick={send}
          />
        </div>
      </div>
      {isDeck && deck.styled && deck.style && <DeckFound style={deck.style} />}
      <div className="hx-hs">
        <button
          type="button"
          className="hx-hs__arrow"
          data-dir="prev"
          aria-label={t('next.designly.hsPrev')}
          tabIndex={-1}
          onClick={() => hsStep(HSCROLL_DIR.prev)}
        >
          <Icon name="chevron-left" size="sm" />
        </button>
        <button
          type="button"
          className="hx-hs__arrow"
          data-dir="next"
          aria-label={t('next.designly.hsNext')}
          tabIndex={-1}
          onClick={() => hsStep(HSCROLL_DIR.next)}
        >
          <Icon name="chevron-right" size="sm" />
        </button>
        <div
          ref={startsRef}
          className="hx-dl__starts hx-hs__track"
          role="radiogroup"
          aria-label={t('next.designly.kind')}
        >
          {KINDS.map((k) => (
            <button
              key={k}
              type="button"
              className="hx-dl-start"
              role="radio"
              aria-checked={k === kind}
              onClick={() => {
                setKind(k);
                inputRef.current?.focus();
              }}
            >
              <span className="hx-dl-start__ic">
                <Icon name={KIND_ICON[k]} />
              </span>
              <span className="hx-dl-start__text">
                <span className="hx-dl-start__name">{t(`next.designly.kinds.${k}`)}</span>
                <span className="hx-dl-start__desc">{t(`next.designly.kindsDesc.${k}`)}</span>
              </span>
            </button>
          ))}
          {onAppKind &&
            appKinds?.map((k) => (
              <button
                key={k.id}
                type="button"
                className="hx-dl-start"
                aria-busy={making === k.id || undefined}
                disabled={making !== null}
                onClick={() => {
                  setMaking(k.id);
                  void onAppKind(k, brief.trim())
                    .then(
                      () => onBrief(''),
                      () => undefined,
                    )
                    .finally(() => setMaking(null));
                }}
              >
                <span className="hx-dl-start__ic">
                  <Icon name={making === k.id ? 'loader-circle' : 'blocks'} />
                </span>
                <span className="hx-dl-start__text">
                  <span className="hx-dl-start__name">{k.title}</span>
                  <span className="hx-dl-start__desc">{k.appName}</span>
                </span>
              </button>
            ))}
        </div>
      </div>
      {isDeck && (
        <DeckSettings
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
          settings={deck.settings}
          onChange={deck.changeSettings}
          styleName={deck.style?.name ?? null}
          anchor={box}
          systems={offered}
          kit={kit}
          ds={ds}
          onDs={deck.chooseSystem}
          template={{
            label: t('next.deck.create.dsTemplate'),
            desc: deck.style?.name ?? t('next.deck.create.dsTemplateDesc'),
            active: deck.styled,
            onPick: deck.chooseTemplate,
          }}
        />
      )}
      {isVideo && (
        <VideoSettings
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
          anchor={box}
          settings={videoSettings}
          onChange={setVideoSettings}
          systems={offered}
          ds={ds}
          onDs={setDs}
        />
      )}
      {kind === DESIGN_KIND.site && (
        <CreatePanel
          open={settingsOpen}
          onClose={() => setSettingsOpen(false)}
          anchor={box}
          label={t('next.deck.create.settingsChip')}
        >
          <DsChoice systems={offered} value={ds} onChange={setDs} kit={kit} />
        </CreatePanel>
      )}
    </div>
  );
};

/** The line under a card's title: the type and the date for an app's document, else the boards, or that it is drawn. */
const cardSub = (card: IDesignCard, now: number): string => {
  const ago = formatAgoFull(card.updatedAt, now);
  if (card.appKind) return `${card.appKind} · ${ago}`;
  if (card.drawing) return t('next.designly.drawingShort');
  return `${plural(card.boards, `next.designly.units.${card.kind}`)} · ${ago}`;
};

interface IDesignCardProps {
  card: IDesignCard;
  index: number;
  now: number;
  pinning: boolean;
  deletable: boolean;
  /** Folders exist: the menu offers «Переместить в…». */
  movable: boolean;
  selecting: boolean;
  selected: boolean;
  onOpen: (id: string) => void;
  onPin: (card: IDesignCard) => void;
  onDelete: (card: IDesignCard) => void;
  onMove: (card: IDesignCard) => void;
  onSelect: (card: IDesignCard) => void;
}

/** «…» of a card: move it, select it, delete it (the owner's). The menu is drawn in a portal, but a click bubbles through
 *  React's tree to the card: each item stops it, or the card opens its project under the dialog the item opened. */
const CardMenu = ({
  card,
  deletable,
  movable,
  onDelete,
  onMove,
  onSelect,
}: Pick<IDesignCardProps, 'card' | 'deletable' | 'movable' | 'onDelete' | 'onMove' | 'onSelect'>) => {
  const actions = useMenuActions();
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger
        className="hx-dl-card__del"
        aria-label={t('next.designly.cardMenu', { title: card.title })}
        onClick={(event) => event.stopPropagation()}
      >
        <Icon name="ellipsis" size="sm" />
      </Menu.Trigger>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={card.title}
        side="bottom"
        align="start"
        sideOffset={6}
      >
        {movable && (
          <Menu.Item
            className="hx-cmenu__item"
            onClick={(event) => {
              event.stopPropagation();
              onMove(card);
            }}
          >
            <Icon name="folder-input" size="sm" />
            <span className="hx-cmenu__txt">{t('next.designly.moveTo')}</span>
          </Menu.Item>
        )}
        <Menu.Item
          className="hx-cmenu__item"
          onClick={(event) => {
            event.stopPropagation();
            onSelect(card);
          }}
        >
          <Icon name="list-checks" size="sm" />
          <span className="hx-cmenu__txt">{t('next.designly.select')}</span>
        </Menu.Item>
        {deletable && (
          <>
            <Menu.Separator className="hx-cmenu__sep" />
            <Menu.Item
              className="hx-cmenu__item"
              data-danger="true"
              onClick={(event) => {
                event.stopPropagation();
                onDelete(card);
              }}
            >
              <Icon name="trash-2" size="sm" />
              <span className="hx-cmenu__txt">{t('next.designly.delete')}</span>
            </Menu.Item>
          </>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

/** One card of the library. Memoized: a keystroke in the field above does not draw the library again. */
const DesignCard = memo(
  ({
    card,
    index,
    now,
    pinning,
    deletable,
    movable,
    selecting,
    selected,
    onOpen,
    onPin,
    onDelete,
    onMove,
    onSelect,
  }: IDesignCardProps) => (
    <div
      data-id={card.id}
      className="hx-dl-card"
      data-state={card.drawing ? 'gen' : 'ready'}
      data-pinned={card.pinned}
      data-selecting={selecting || undefined}
      data-selected={selected || undefined}
      data-i={Math.min(index, 8)}
      tabIndex={0}
      role="button"
      aria-label={card.title}
      onClick={(event) => {
        // The card's «…» menu sits in a portal, yet React bubbles its clicks here: a menu item must not open or toggle the card.
        if (!event.currentTarget.contains(event.target as Node)) return;
        if (selecting || event.shiftKey || event.metaKey || event.ctrlKey) onSelect(card);
        else onOpen(card.id);
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' && event.target === event.currentTarget) {
          if (selecting) onSelect(card);
          else onOpen(card.id);
        }
      }}
    >
      <div className="hx-dl-card__shot">
        <CardShot image={card.image} />
        <span className="hx-dl-card__alt" />
        {!card.appKind && (
          <button
            type="button"
            className="hx-dl-card__pin hx-tipped"
            aria-pressed={card.pinned}
            aria-busy={pinning || undefined}
            aria-label={card.pinned ? t('next.designly.unpin') : t('next.designly.pin')}
            disabled={pinning}
            onClick={(event) => {
              event.stopPropagation();
              onPin(card);
            }}
          >
            <Icon name="pin" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {card.pinned ? t('next.designly.unpin') : t('next.designly.pin')}
            </span>
          </button>
        )}
        {!selecting && !card.appKind && (movable || deletable) && (
          <CardMenu
            card={card}
            deletable={deletable}
            movable={movable}
            onDelete={onDelete}
            onMove={onMove}
            onSelect={onSelect}
          />
        )}
        {selecting && (
          <span className="hx-dl-card__check" aria-hidden="true">
            <Icon name="check" size="sm" />
          </span>
        )}
        <span className="hx-dl-card__gen">
          <Icon name="loader-circle" size="sm" />
          {t('next.designly.drawing')}
        </span>
      </div>
      <div className="hx-dl-card__meta">
        <span className="hx-dl-card__text">
          <span className="hx-dl-card__title">{card.title}</span>
          <span className="hx-dl-card__sub">
            <Icon name={card.appKind ? 'blocks' : KIND_ICON[card.kind]} size="sm" />
            {cardSub(card, now)}
          </span>
        </span>
        {card.owner && (
          <span
            className="hx-dl-card__who"
            role="img"
            aria-label={t('next.designly.owner', { name: card.owner.name })}
            title={t('next.designly.owner', { name: card.owner.name })}
          >
            {initialOf(card.owner.name)}
          </span>
        )}
      </div>
    </div>
  ),
);
DesignCard.displayName = 'DesignCard';

const DesignlyHome = ({
  cards,
  error,
  now,
  onPin,
  onDelete,
  organizer,
  onRestoreDemo,
  onOpen,
  onRetry,
  compose,
  kits,
}: IDesignlyHomeProps) => {
  const [pinning, setPinning] = useState<string | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [query, setQuery] = useState('');
  const [filters, setFilters] = useState<ILibraryFilters>(NO_FILTERS);
  // The folder open ('' is the library itself), the trash, the selection and the dialogs.
  const [folder, setFolder] = useState('');
  const [trashOpen, setTrashOpen] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [moving, setMoving] = useState<string[] | null>(null);
  const [folderForm, setFolderForm] = useState<IFolderForm | null>(null);
  const [folderDelete, setFolderDelete] = useState<IFolder | null>(null);
  const [gridEl, setGridEl] = useState<HTMLDivElement | null>(null);
  const selbar = usePresentFlag(selecting, 200);
  const [scope, setScope] = useState<TDesignScope>(DESIGN_SCOPE.all);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const gridRef = useRef<HTMLDivElement>(null);
  const flipFrom = useRef<ReadonlyMap<string, DOMRect> | null>(null);
  const rootRef = useRef<HTMLElement>(null);

  // Scrolled: --p is how far the field went (0…1), data-stuck says the library's row is at the top. Once a frame,
  // reads first, then writes; --p goes on the hero only, so the cards are not recomputed.
  useEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    let frame = 0;
    let last = '';
    const measure = () => {
      frame = 0;
      const hero = el.querySelector<HTMLElement>('.hx-dl__hero');
      const bar = el.querySelector<HTMLElement>('.hx-dl__toolbar');
      if (!bar) return;
      const top = el.scrollTop;
      const stuck = top > 0 && bar.getBoundingClientRect().top - el.getBoundingClientRect().top <= 0.5;
      // The library alone (the pane's) has no field to move away.
      const p = hero ? Math.min(1, Math.max(0, top / Math.max(1, hero.offsetHeight))).toFixed(3) : '0';
      if (hero && p !== last) {
        last = p;
        hero.style.setProperty('--p', p);
      }
      if (el.dataset.stuck !== String(stuck)) el.dataset.stuck = String(stuck);
    };
    const onScroll = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    el.addEventListener('scroll', onScroll, { passive: true });
    measure();
    return () => {
      el.removeEventListener('scroll', onScroll);
      cancelAnimationFrame(frame);
    };
  }, []);

  const toTop = () => {
    rootRef.current?.scrollTo({ top: 0, behavior: 'smooth' });
    window.setTimeout(() => inputRef.current?.focus({ preventScroll: true }), 420);
  };

  const q = query.trim().toLowerCase();
  const organizedFolders = organizer?.folders;
  const folders = useMemo(() => organizedFolders ?? [], [organizedFolders]);
  const organized = organizer !== undefined;
  // A search or a filter looks through every folder; with none, the library shows the folder that is open.
  const searching = q !== '' || isNarrowed(filters);
  const here = folders.some((f) => f.id === folder) ? folder : '';
  const shown = useMemo(() => {
    if (cards === null) return null;
    const list = cards.filter(
      (c) =>
        inScope(scope, c.owner) &&
        (!q || c.title.toLowerCase().includes(q)) &&
        matchesFilters(c, filters, now) &&
        (searching || !organizer || (c.folder ?? '') === here),
    );
    return sortCards(list, filters.sort);
  }, [cards, scope, q, filters, now, searching, organizer, here]);
  const systems = useMemo(() => systemsOf(cards ?? []), [cards]);
  const subfolders = !searching && !trashOpen ? childrenOf(folders, here) : [];
  const projectIds = useMemo(
    () => new Set((cards ?? []).filter((c) => !c.appKind).map((c) => c.id)),
    [cards],
  );

  // The tour's lessons lead to the demo projects: the library says where each lies and what hides it, and
  // lets the tour clear the view or copy the demo back. Only the library page does (the pane's has no lessons).
  const lessons = compose !== null;
  const restoreNow = useStableFn(async () => {
    if (onRestoreDemo) await onRestoreDemo();
  });
  useEffect(() => {
    if (!lessons) return;
    return registerLibraryControl({
      clearView: () => {
        setQuery('');
        setFilters(NO_FILTERS);
        setScope(DESIGN_SCOPE.all);
        setTrashOpen(false);
        setFolder('');
        setSelecting(false);
        setSelected(new Set());
      },
      restoreDemo: async () => {
        setQuery('');
        setFilters(NO_FILTERS);
        setScope(DESIGN_SCOPE.all);
        setTrashOpen(false);
        setFolder('');
        await restoreNow();
      },
    });
  }, [lessons, restoreNow]);
  useEffect(() => {
    if (!lessons || cards === null) return;
    const names = DEMO_IDS.map((id) => {
      const card = cards.find((c) => c.id === id);
      const matches =
        card !== undefined &&
        inScope(scope, card.owner) &&
        (!q || card.title.toLowerCase().includes(q)) &&
        matchesFilters(card, filters, now);
      const way = routeToCard(card, {
        folders,
        here,
        trashOpen,
        matches,
        searching,
        organized,
      });
      return `lib:route:${id}:${way.route}${way.folder ? `:${way.folder}` : ''}`;
    });
    tourFactGroup('lib:route:', names);
  }, [lessons, cards, scope, q, filters, now, folders, here, trashOpen, searching, organized]);
  useEffect(() => () => tourFactGroup('lib:route:', []), []);

  // Pinning: the cards were measured before the machine kept the pin; when the new order is on screen, each one
  // travels from its old place (FLIP: all reads first, then the animations). Not with reduced motion.
  const order = shown?.map((c) => c.id).join('|') ?? '';
  useLayoutEffect(() => {
    const from = flipFrom.current;
    const grid = gridRef.current;
    if (!from || !grid) return;
    flipFrom.current = null;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    const cardsNow = [...grid.children].filter((el): el is HTMLElement => el instanceof HTMLElement);
    const rects = cardsNow.map((el) => el.getBoundingClientRect());
    cardsNow.forEach((el, i) => {
      const before = from.get(el.dataset.id ?? '');
      const now = rects[i];
      if (!before || !now) return;
      const dx = before.left - now.left;
      const dy = before.top - now.top;
      if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return;
      el.animate([{ translate: `${dx}px ${dy}px` }, { translate: '0 0' }], {
        duration: FLIP_MS,
        easing: FLIP_EASE,
      });
    });
  }, [order]);

  const open = useStableFn(onOpen);
  const pin = useStableFn((card: IDesignCard) => {
    const grid = gridRef.current;
    const rects = new Map<string, DOMRect>();
    for (const el of grid?.children ?? []) {
      if (el instanceof HTMLElement && el.dataset.id) rects.set(el.dataset.id, el.getBoundingClientRect());
    }
    flipFrom.current = rects;
    setPinning(card.id);
    void onPin(card.id, !card.pinned)
      .catch(() => {
        flipFrom.current = null;
      })
      .finally(() => {
        setPinning(null);
        window.setTimeout(() => {
          flipFrom.current = null;
        }, FLIP_KEEP_MS);
      });
  });

  const deletableIds = (ids: readonly string[]) =>
    ids.filter((id) => cards?.find((c) => c.id === id)?.canDelete === true);
  const deleteIds = (ids: string[]): Promise<void> => {
    if (organizer) return organizer.onDeleteMany(ids);
    return ids.reduce<Promise<void>>((chain, id) => chain.then(() => onDelete?.(id)), Promise.resolve());
  };
  const canDeleteHere = organizer !== undefined || onDelete !== undefined;

  const stopSelecting = () => {
    setSelecting(false);
    setSelected(new Set());
  };
  const toggle = useStableFn((card: IDesignCard) => {
    if (card.appKind) return;
    setSelecting(true);
    setSelected((prev) => {
      const next = new Set(prev);
      if (!next.delete(card.id)) next.add(card.id);
      return next;
    });
  });
  const moveOne = useStableFn((card: IDesignCard) => setMoving([card.id]));
  const deleteOne = useStableFn((card: IDesignCard) => void deleteIds([card.id]).catch(() => undefined));
  const dropped = (ids: string[], target: string) => {
    if (!organizer) return;
    organizer.onMove(ids, target).then(stopSelecting, () => undefined);
  };
  useCardDrag(gridEl, {
    enabled: organizer !== undefined && !trashOpen,
    carry: (id) => (selected.has(id) ? [...selected] : [id]),
    onDrop: dropped,
  });
  const openFolder = (id: string) => {
    setFolder(id);
    stopSelecting();
  };
  const newFolder = () => {
    const current = folders.find((f) => f.id === here);
    setFolderForm({ parent: current ? (current.parent === '' ? current.id : current.parent) : '' });
  };
  const movingFrom = (ids: string[] | null): string | null => {
    const places = new Set((ids ?? []).map((id) => cards?.find((c) => c.id === id)?.folder ?? ''));
    return places.size === 1 ? ([...places][0] ?? null) : null;
  };

  return (
    <section className="hx-dl" ref={rootRef} data-library={compose === null}>
      {compose && <Hero {...compose} inputRef={inputRef} />}

      <div className="hx-dl__body">
        <div className="hx-dl__toolbar">
          <div className="hx-dl__heading">{t('next.designly.library')}</div>
          <div className="hx-dl__tabs" role="tablist" aria-label={t('next.designly.scope')}>
            {DESIGN_SCOPES.map((k) => (
              <button
                key={k}
                type="button"
                role="tab"
                className="hx-dl__tab"
                aria-selected={k === scope}
                onClick={() => setScope(k)}
              >
                {t(`next.designly.scopes.${k}`)}
              </button>
            ))}
          </div>
          <span className="hx-spacer" />
          <label className="hx-dl__search" data-filled={query !== ''}>
            <Icon name="search" size="sm" />
            <input
              type="search"
              placeholder={t('next.designly.search')}
              autoComplete="off"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          {!trashOpen && (
            <LibraryBar
              filters={filters}
              onFilters={setFilters}
              systems={systems}
              tools={
                organizer
                  ? {
                      selecting,
                      onNewFolder: newFolder,
                      onSelect: () => (selecting ? stopSelecting() : setSelecting(true)),
                      onTrash: () => {
                        stopSelecting();
                        setTrashOpen(true);
                      },
                    }
                  : null
              }
            />
          )}
          {compose && (
            <button type="button" className="hx-dl__new" tabIndex={-1} onClick={toTop}>
              <Icon name="plus" size="sm" />
              {t('next.designly.new')}
            </button>
          )}
        </div>

        {trashOpen && organizer && (
          <TrashView
            items={organizer.trash}
            now={now}
            load={organizer.loadTrash}
            onBack={() => setTrashOpen(false)}
            onRestore={organizer.onRestore}
            onPurge={organizer.onPurge}
          />
        )}

        {!trashOpen && kits && !searching && here === '' && <KitShelf {...kits} />}
        {!trashOpen && organizer && !searching && here !== '' && (
          <Crumbs folders={folders} current={here} onOpen={openFolder} />
        )}
        {!trashOpen && organizer && subfolders.length > 0 && (
          <FolderTiles
            folders={subfolders}
            onOpen={openFolder}
            onRename={(f) => setFolderForm({ folder: f, parent: f.parent })}
            onDelete={setFolderDelete}
          />
        )}

        {!trashOpen && cards === null && error !== null && (
          <LoadFailed
            size={LOAD_FAILED_SIZE.page}
            title={t('next.designly.failed')}
            reason={error}
            onRetry={onRetry}
          />
        )}
        {!trashOpen && (cards !== null || error === null) && (
          <Swap ready={shown !== null} skeleton={<LibrarySkeleton />}>
            {shown && shown.length > 0 && (
              <div
                className="hx-dl__grid"
                ref={(el) => {
                  gridRef.current = el;
                  setGridEl(el);
                }}
              >
                {shown.map((card, i) => (
                  <DesignCard
                    key={card.id}
                    card={card}
                    index={i}
                    now={now}
                    pinning={pinning === card.id}
                    deletable={canDeleteHere && card.canDelete === true}
                    movable={organizer !== undefined}
                    selecting={selecting}
                    selected={selected.has(card.id)}
                    onOpen={open}
                    onPin={pin}
                    onDelete={deleteOne}
                    onMove={moveOne}
                    onSelect={toggle}
                  />
                ))}
              </div>
            )}
          </Swap>
        )}
        {!trashOpen && shown && shown.length === 0 && (
          <div className="hx-dl__empty">
            {t(
              here !== '' && !searching
                ? 'next.designly.emptyFolder'
                : emptyKey(scope, q !== '' || isNarrowed(filters)),
            )}
          </div>
        )}
        {compose &&
          cards !== null &&
          !trashOpen &&
          !searching &&
          here === '' &&
          scope === DESIGN_SCOPE.all && (
            <nav className="hx-dl__lessons" aria-label={t('next.tour.lessons')}>
              <span className="hx-dl__lessons-hint">{t('next.tour.lessonsHint')}</span>
              {TOUR_ORDER.map((k) => (
                <button key={k} type="button" className="hx-dl__lesson" onClick={() => startTour(k)}>
                  {t(`next.tour.${k}.lesson`)}
                </button>
              ))}
            </nav>
          )}
        {onRestoreDemo &&
          cards !== null &&
          !trashOpen &&
          !searching &&
          here === '' &&
          scope === DESIGN_SCOPE.all &&
          demoMissing(cards) && (
            <button
              type="button"
              className="hx-dl__demo"
              disabled={restoring}
              onClick={() => {
                setRestoring(true);
                onRestoreDemo()
                  .catch(() => undefined)
                  .finally(() => setRestoring(false));
              }}
            >
              {t('next.designly.restoreDemo')}
            </button>
          )}
      </div>
      {organizer && selbar.mounted && (
        <div
          className="hx-dl__selbar"
          data-state={selbar.state}
          role="toolbar"
          aria-label={t('next.designly.selection')}
        >
          <span className="hx-dl__selbar-n">{plural(selected.size, 'next.designly.selected')}</span>
          <button
            type="button"
            className="hx-dl__chip"
            disabled={selected.size === 0}
            onClick={() => setMoving([...selected])}
          >
            <Icon name="folder-input" size="sm" />
            {t('next.designly.moveTo')}
          </button>
          <button
            type="button"
            className="hx-dl__chip"
            data-danger="true"
            disabled={deletableIds([...selected]).length === 0}
            onClick={() => {
              void deleteIds(deletableIds([...selected])).then(stopSelecting, () => undefined);
            }}
          >
            <Icon name="trash-2" size="sm" />
            {t('next.designly.delete')}
          </button>
          <button type="button" className="hx-dl__chip" onClick={stopSelecting}>
            {t('next.designly.done')}
          </button>
        </div>
      )}
      {organizer && (
        <>
          <FolderNameDialog
            form={folderForm}
            onClose={() => setFolderForm(null)}
            onSave={organizer.onFolderSave}
          />
          <FolderDeleteDialog
            folder={folderDelete}
            projects={folderDelete ? projectsIn(cards ?? [], folders, folderDelete.id) : 0}
            onClose={() => setFolderDelete(null)}
            onDelete={async (id, mode) => {
              await organizer.onFolderDelete(id, mode);
              if (id === here) setFolder('');
            }}
          />
          <MoveDialog
            ids={moving}
            folders={folders}
            from={movingFrom(moving)}
            onClose={() => setMoving(null)}
            onMove={async (ids, target) => {
              await organizer.onMove(
                ids.filter((id) => projectIds.has(id)),
                target,
              );
              stopSelecting();
            }}
          />
        </>
      )}
    </section>
  );
};

export default DesignlyHome;
