// A link without sign-in as its screen edits it (stage «г»): the form, what it sends to the gateway and the
// line a link shows in the list. One model for every kind; what differs is what «Скачивание» and «Ещё» hold:
// a design — how it opens (computer or phone), its pages, the version; a deck — how it opens, from which slide, the
// speaker's notes, counting, its PDF; a video — the format (only where both are built), autoplay, comments on the
// timeline; a document — downloading and copying its text. Pure: the screen keeps the state, this says what it means.
import type { IGuestView } from '@/next/features/designly/access/api';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import { shortDay } from '@/next/features/designly/access/labels';
import type {
  IAccessLink,
  ILinkChange,
  ILinkInput,
  TLinkScalar,
} from '@/next/features/designly/access/resource-api';
import { deckViewOf, readDeckLinkView } from '@/next/features/designly/deck/deck-link-view';
import { DECK_OPENS, type TDeckOpens } from '@/next/features/designly/deck/types';
import { DEVICE_GROUP, DEVICES } from '@/next/features/designly/devices';
import { FIT } from '@/next/features/designly/site-zoom';
import { readVideoLinkView, videoViewOf } from '@/next/features/designly/video/share-view';
import { VIDEO_FORMATS, type TVideoFormat } from '@/next/features/designly/video/types';
import { t } from '@/next/i18n';

/** `board`: a board of the catalog — a document without text to copy or a file to download. */
export const LINK_KIND = {
  design: 'design',
  deck: 'deck',
  video: 'video',
  doc: 'doc',
  board: 'board',
} as const;
export type TLinkKind = (typeof LINK_KIND)[keyof typeof LINK_KIND];

/** The stops of «Срок»: a day, a week, a month, for ever. */
export const TERMS = ['1d', '7d', '30d', null] as const;
export type TTerm = (typeof TERMS)[number];

export const DEVICE_CHOICE = { desktop: 'desktop', phone: 'phone' } as const;
export type TDeviceChoice = (typeof DEVICE_CHOICE)[keyof typeof DEVICE_CHOICE];

export type TLinkRights = 'view' | 'comment';
export type TLinkVersion = 'latest' | 'pinned';

/** What the screen of a link holds; a field a kind has no use for keeps its default and is not sent. */
export interface ILinkForm {
  forWhom: string;
  rights: TLinkRights;
  /** The chosen stop; null under `termTouched: false` of a link being changed means «as it is». */
  term: TTerm;
  /** A changed link keeps its end unless the term is touched: a step counts from now. */
  termTouched: boolean;
  download: boolean;
  /** A document: the guest may select and copy its text. */
  copy: boolean;
  device: TDeviceChoice;
  /** Board paths; null is all of them. */
  pages: string[] | null;
  opens: TDeckOpens;
  /** 1-based. */
  from: number;
  notes: boolean;
  stats: boolean;
  format: TVideoFormat;
  autoplay: boolean;
  tlComments: boolean;
  version: TLinkVersion;
}

/** What the screen knows of the thing shared. */
export interface ILinkContext {
  kind: TLinkKind;
  /** A design's pages, to give some of them; fewer than two and there is nothing to choose. */
  pages?: readonly IScopePage[];
  /** A deck's slides, for «С какого слайда». */
  slides?: number;
  /** A video's built formats: the format is chosen only where both are. */
  formats?: readonly TVideoFormat[];
  /** The owner's remembered look of a design: a link opens the same way on the device it was looked at on. */
  look?: IGuestView;
}

/** What «Скачивание» gives, by kind; null where the guest page has no file to give yet. */
export const downloadOf = (kind: TLinkKind): string | null => {
  if (kind === LINK_KIND.deck || kind === LINK_KIND.doc) return 'PDF';
  return null;
};

const PHONES = new Set(DEVICES.filter((d) => d.group === DEVICE_GROUP.phones).map((d) => d.id));
const FIRST_PHONE = DEVICES.find((d) => d.group === DEVICE_GROUP.phones && d.vw !== undefined);

const isPhoneDevice = (device: unknown): boolean => typeof device === 'string' && PHONES.has(device);

export const blankForm = (ctx: ILinkContext): ILinkForm => ({
  forWhom: '',
  rights: 'view',
  term: '7d',
  termTouched: true,
  download: false,
  copy: true,
  device: ctx.look && isPhoneDevice(ctx.look.device) ? DEVICE_CHOICE.phone : DEVICE_CHOICE.desktop,
  pages: null,
  opens: DECK_OPENS.slides,
  from: 1,
  notes: false,
  stats: true,
  format: ctx.formats?.[0] ?? VIDEO_FORMATS[0],
  autoplay: false,
  tlComments: true,
  // A design or a document shows what it is now: the owner keeps working, the guest does not see it.
  version: 'pinned',
});

const flag = (v: TLinkScalar | undefined, fallback: boolean): boolean =>
  typeof v === 'boolean' ? v : fallback;

/** The screen of a link being changed. */
export const formOf = (link: IAccessLink, ctx: ILinkContext): ILinkForm => {
  const o = link.options ?? {};
  const blank = blankForm(ctx);
  const deck = readDeckLinkView(o);
  const video = readVideoLinkView(o);
  const from = typeof o.from === 'number' && Number.isInteger(o.from) && o.from >= 1 ? o.from : 1;
  return {
    ...blank,
    forWhom: link.forWhom,
    rights: link.rights,
    term: null,
    termTouched: false,
    download: ctx.kind === LINK_KIND.doc ? flag(o.download, false) : (deck?.pdf ?? false),
    copy: flag(o.copy, true),
    device: isPhoneDevice(o.device) ? DEVICE_CHOICE.phone : DEVICE_CHOICE.desktop,
    pages: link.pages,
    opens: deck?.opens ?? blank.opens,
    from,
    notes: flag(o.notes, false),
    stats: deck?.stats ?? video?.stats ?? flag(o.stats, true),
    format: video?.format ?? blank.format,
    autoplay: flag(o.autoplay, false),
    tlComments: flag(o.tlComments, true),
    version: link.version,
  };
};

/** A design's look for the device chosen: the remembered one where it is that kind of device, else a plain one. */
const designLook = (form: ILinkForm, look: IGuestView | undefined): IGuestView | null => {
  const phone = form.device === DEVICE_CHOICE.phone;
  if (look && isPhoneDevice(look.device) === phone) return look;
  if (!phone) return null;
  return {
    v: 1,
    device: FIRST_PHONE?.id ?? 'ip14',
    rotated: false,
    width: FIRST_PHONE?.vw ?? null,
    height: FIRST_PHONE?.vh ?? null,
    chrome: true,
    browser: null,
    fit: FIT.auto,
    zoom: null,
    panX: null,
    panY: null,
    locked: false,
    page: null,
  };
};

/** What the link allows, as the gateway keeps it (a flat object of scalars). */
export const optionsOf = (form: ILinkForm, ctx: ILinkContext): Record<string, TLinkScalar> | null => {
  if (ctx.kind === LINK_KIND.doc) return { download: form.download, copy: form.copy };
  if (ctx.kind === LINK_KIND.board) return {};
  if (ctx.kind === LINK_KIND.deck) {
    const slides = ctx.slides ?? 0;
    const from = slides > 0 ? Math.min(form.from, slides) : 1;
    return {
      ...deckViewOf({ opens: form.opens, pdf: form.download, stats: form.stats }),
      from,
      notes: form.notes,
    };
  }
  if (ctx.kind === LINK_KIND.video) {
    const both = (ctx.formats?.length ?? 0) > 1;
    const format = both ? form.format : (ctx.formats?.[0] ?? form.format);
    return {
      ...videoViewOf({ stats: form.stats, format }),
      autoplay: form.autoplay,
      // Comments on the timeline are there only for a link that may comment.
      tlComments: form.rights === 'comment' && form.tlComments,
    };
  }
  const look = designLook(form, ctx.look);
  return look ? { ...look } : { device: 'fill' };
};

const pagesOf = (form: ILinkForm, ctx: ILinkContext): string[] | null =>
  ctx.kind === LINK_KIND.design && form.pages !== null && form.pages.length < (ctx.pages?.length ?? 0)
    ? form.pages
    : null;

export const inputOf = (form: ILinkForm, ctx: ILinkContext): ILinkInput => ({
  forWhom: form.forWhom.trim(),
  rights: form.rights,
  ttl: form.term,
  options: optionsOf(form, ctx),
  version: form.version,
  pages: pagesOf(form, ctx),
});

/** A change of a link: everything but the term, which is sent only when touched. */
export const changeOf = (form: ILinkForm, ctx: ILinkContext): ILinkChange => {
  const { ttl, ...rest } = inputOf(form, ctx);
  return form.termTouched ? { ...rest, ttl } : rest;
};

/** Whether nothing but the screen can be read: a document that neither downloads nor copies. */
export const readOnly = (form: ILinkForm, kind: TLinkKind): boolean =>
  kind === LINK_KIND.doc && !form.download && !form.copy;

const MS_DAY = 86_400_000;

const dayStart = (ms: number) => {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};

/** «ещё не открывали», «открывали сегодня», «открывали вчера», «открывали 28.10». */
export const seenLabel = (link: IAccessLink, now: number): string => {
  if (link.opens === 0 || link.lastOpenedAt === null) return t('next.access.links.seenNever');
  const at = Date.parse(link.lastOpenedAt);
  if (Number.isNaN(at)) return t('next.access.links.seenNever');
  const days = Math.round((dayStart(now) - dayStart(at)) / MS_DAY);
  if (days <= 0) return t('next.access.links.seenToday');
  if (days === 1) return t('next.access.links.seenYesterday');
  return t('next.access.links.seenOn', { date: shortDay(link.lastOpenedAt) ?? '' });
};

export const isOff = (link: IAccessLink, now: number): boolean =>
  link.disabledAt !== null && Date.parse(link.disabledAt) <= now;

export const ranOut = (link: IAccessLink, now: number): boolean =>
  link.expiresAt !== null && Date.parse(link.expiresAt) <= now;

/** Whether the guest can copy the document's text. */
export const noCopy = (link: IAccessLink, kind: TLinkKind): boolean =>
  kind === LINK_KIND.doc && link.options?.copy === false;

/** What a link may download now, by kind. */
const downloads = (link: IAccessLink, kind: TLinkKind): boolean => {
  if (kind === LINK_KIND.doc) return link.options?.download === true;
  if (kind === LINK_KIND.deck) return link.options?.pdf === true;
  return false;
};

/** The line under a link's name: «смотреть · PDF · до 11.11 · открывали вчера», «отключена · …». */
export const linkSub = (link: IAccessLink, kind: TLinkKind, now: number): string => {
  const seen = seenLabel(link, now);
  if (isOff(link, now)) return [t('next.access.links.off'), seen].join(' · ');
  const file = downloadOf(kind);
  const until = (() => {
    if (link.expiresAt === null) return '';
    if (ranOut(link, now)) return t('next.access.links.ranOut');
    const day = shortDay(link.expiresAt);
    return day === null ? '' : t('next.access.links.until', { date: day });
  })();
  return [
    t(`next.access.links.rights.${link.rights}`),
    file !== null && downloads(link, kind) ? file : '',
    noCopy(link, kind) ? t('next.access.links.noCopy') : '',
    until,
    seen,
  ]
    .filter(Boolean)
    .join(' · ');
};

/** «до 11 ноября» of a stop, from now; «Без срока» for ever. */
// One formatter per language: toLocaleDateString builds a new one per call, and the slider of a term asks for each step.
const DAY_MONTH = new Map<string, Intl.DateTimeFormat>();
const dayMonth = (locale: string): Intl.DateTimeFormat => {
  const known = DAY_MONTH.get(locale);
  if (known) return known;
  const made = new Intl.DateTimeFormat(locale, { day: 'numeric', month: 'long' });
  DAY_MONTH.set(locale, made);
  return made;
};

export const termDate = (term: TTerm, now: number): string => {
  if (term === null) return t('next.access.links.termForever');
  const days = Number.parseInt(term, 10);
  const end = new Date(now + days * MS_DAY);
  return t('next.access.links.termUntil', { date: dayMonth(t('next.access.links.locale')).format(end) });
};

/** The short word of «Ещё» on its closed row. */
export const moreSummary = (form: ILinkForm, ctx: ILinkContext): string => {
  const version = t(`next.access.links.version.${form.version}`);
  if (ctx.kind === LINK_KIND.design) {
    const total = ctx.pages?.length ?? 0;
    const some = form.pages !== null && form.pages.length < total;
    return [
      t(`next.access.links.device.${form.device}`),
      some
        ? t('next.access.links.pagesSome', { count: form.pages?.length ?? 0 })
        : t('next.access.links.pagesAll'),
    ].join(' · ');
  }
  if (ctx.kind === LINK_KIND.deck) {
    return [
      t(`next.access.links.opens.${form.opens}`),
      form.from <= 1 ? t('next.access.links.fromStart') : t('next.access.links.fromSlide', { n: form.from }),
    ].join(' · ');
  }
  if (ctx.kind === LINK_KIND.video) {
    const formats = ctx.formats ?? [];
    return [formats.length > 1 ? form.format : (formats[0] ?? form.format), version].join(' · ');
  }
  return version;
};
