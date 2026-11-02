// Which apps the editor shows for a document, and what of theirs: pure, so the rules are tried without a page.
import type { IInstalledApp } from '@/next/features/app-views/api';
import { targetAllowed } from '@/next/features/app-views/bridge';
import type {
  ISelectedNode,
  ISinopia,
  ISinopiaBindings,
  ISinopiaBoard,
  ISinopiaMark,
  ISinopiaPanel,
  ISinopiaSelectionItem,
  TMarkLevel,
} from '@/next/features/app-views/sinopia/types';
import { MARK_LEVELS } from '@/next/features/app-views/sinopia/types';

/** An empty list of kinds is every kind of the app's. */
export const kindIn = (kinds: readonly string[] | undefined, kind: string): boolean =>
  kinds === undefined || kinds.length === 0 || kinds.includes(kind);

/** An app on in a document, with what it puts into a document of the kind. */
export interface IActiveApp {
  app: IInstalledApp;
  panels: ISinopiaPanel[];
  selection: ISinopiaSelectionItem[];
  bindings: ISinopiaBindings | null;
}

/**
 * The apps the editor shows for a document of a kind. `on` is the runtime's list of the apps on in the document (it
 * holds the rule: `activation.on` has «document» and the app's targets name `sinopia.<kind>`); the page checks the
 * target once more so that a list from an older runtime cannot put an app where it does not work. A panel is shown
 * only if its view is the app's and made for this kind of document.
 */
export const activeApps = (
  apps: readonly IInstalledApp[],
  on: readonly string[],
  kind: string,
): IActiveApp[] => {
  const out: IActiveApp[] = [];
  for (const app of apps) {
    if (!on.includes(app.id) || !targetAllowed(app.targets, `sinopia.${kind}`)) continue;
    const sin: ISinopia = app.sinopia ?? {};
    const viewOk = (id: string) =>
      app.views.some((v) => v.id === id && targetAllowed(v.targets, `sinopia.${kind}`));
    out.push({
      app,
      panels: (sin.panels ?? []).filter((p) => kindIn(p.kinds, kind) && viewOk(p.view)),
      selection: (sin.selection ?? []).filter((s) => kindIn(s.kinds, kind)),
      bindings: sin.bindings && viewOk(sin.bindings.inspector) ? sin.bindings : null,
    });
  }
  return out;
};

export interface IPanelRef {
  /** Unique in the document: `<app>/<panel>`. */
  key: string;
  app: IInstalledApp;
  view: string;
  title: string;
}

export const panelsOf = (active: readonly IActiveApp[]): IPanelRef[] =>
  active.flatMap((a) =>
    a.panels.map((p) => ({
      key: `${a.app.id}/${p.id}`,
      app: a.app,
      view: p.view,
      title: p.title ?? a.app.name,
    })),
  );

export interface ISelectionAction {
  key: string;
  app: IInstalledApp;
  item: ISinopiaSelectionItem;
}

export const selectionActions = (active: readonly IActiveApp[]): ISelectionAction[] =>
  active.flatMap((a) => a.selection.map((item) => ({ key: `${a.app.id}/${item.id}`, app: a.app, item })));

/**
 * The app whose field a click on a bound element opens. An element may say which app it is bound to (`data-hl-app`);
 * otherwise the first app with bindings is the one.
 */
export const inspectorFor = (
  active: readonly IActiveApp[],
  hl: Record<string, string> | undefined,
): { app: IInstalledApp; view: string } | null => {
  const withBindings = active.filter(
    (a): a is IActiveApp & { bindings: ISinopiaBindings } => a.bindings !== null,
  );
  const named = hl?.app === undefined ? undefined : withBindings.find((a) => a.app.id === hl.app);
  const chosen = named ?? withBindings[0];
  return chosen ? { app: chosen.app, view: chosen.bindings.inspector } : null;
};

/** The board a frame at an address shows: its file is the address's path after `/p/`. */
export const boardAt = (boards: readonly ISinopiaBoard[], href: string): ISinopiaBoard | null => {
  let path: string;
  try {
    path = new URL(href).pathname;
  } catch {
    return null;
  }
  const at = path.indexOf('/p/');
  if (at < 0) return null;
  const file = path
    .slice(at + 3)
    .split('/')
    .map((part) => {
      try {
        return decodeURIComponent(part);
      } catch {
        return part;
      }
    })
    .join('/');
  return boards.find((b) => b.file === file) ?? null;
};

/** A node id is `<page>:<number>`. */
export const parseNodeId = (id: string): { page: string; tpl: number } | null => {
  const at = id.lastIndexOf(':');
  if (at <= 0) return null;
  const tpl = Number(id.slice(at + 1));
  return Number.isInteger(tpl) && tpl >= 0 && /^\d+$/.test(id.slice(at + 1))
    ? { page: id.slice(0, at), tpl }
    : null;
};

export interface IBoardMark {
  tpl: number;
  level: TMarkLevel;
  note: string;
}

const RANK: Record<TMarkLevel, number> = { error: 0, warning: 1, info: 2, comment: 3 };

/** The marks of a board, one per element (the worst level; the notes of an element's marks together), as the board draws them. */
export const marksOfBoard = (marks: readonly ISinopiaMark[], page: string): IBoardMark[] => {
  const byTpl = new Map<number, IBoardMark>();
  for (const m of marks) {
    const id = parseNodeId(m.node);
    if (id === null || id.page !== page) continue;
    const level: TMarkLevel = MARK_LEVELS.includes(m.level) ? m.level : 'info';
    const had = byTpl.get(id.tpl);
    if (!had) byTpl.set(id.tpl, { tpl: id.tpl, level, note: m.note });
    else
      byTpl.set(id.tpl, {
        tpl: id.tpl,
        level: RANK[level] < RANK[had.level] ? level : had.level,
        note: had.note.includes(m.note) ? had.note : `${had.note}\n${m.note}`,
      });
  }
  return [...byTpl.values()];
};

/** What a click on a bound element of a board says (`tk:hl-click`), as read from the board's message. */
export interface IHlClick {
  field: string;
  hl: Record<string, string>;
  tpl: number | null;
  fp: string;
  selector: string;
  tag: string;
  text: string;
  rect: { x: number; y: number; width: number; height: number };
  url: string;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

const readHl = (raw: unknown): Record<string, string> => {
  const out: Record<string, string> = {};
  if (!isRecord(raw)) return out;
  for (const [name, value] of Object.entries(raw).slice(0, 12)) {
    if (/^[a-z0-9-]{1,40}$/.test(name) && typeof value === 'string') out[name] = value.slice(0, 200);
  }
  return out;
};

/** The board's `tk:hl-click`, or null when it is not one. The board's words are data: clipped, never trusted for more. */
export const readHlClick = (data: unknown): IHlClick | null => {
  if (!isRecord(data) || data.type !== 'tk:hl-click') return null;
  if (typeof data.field !== 'string' || data.field === '' || data.field.length > 80) return null;
  const rect = isRecord(data.rect) ? data.rect : {};
  if (!isNum(rect.x) || !isNum(rect.y) || !isNum(rect.width) || !isNum(rect.height)) return null;
  if (typeof data.url !== 'string') return null;
  return {
    field: data.field,
    hl: readHl(data.hl),
    tpl: Number.isInteger(data.tpl) && (data.tpl as number) >= 0 ? (data.tpl as number) : null,
    fp: typeof data.fp === 'string' ? data.fp.slice(0, 64) : '',
    selector: typeof data.selector === 'string' ? data.selector.slice(0, 512) : '',
    tag: typeof data.tag === 'string' ? data.tag.slice(0, 32) : '',
    text: typeof data.text === 'string' ? data.text.slice(0, 120) : '',
    rect: { x: rect.x, y: rect.y, width: rect.width, height: rect.height },
    url: data.url.slice(0, 2048),
  };
};

/** The board's `tk:hl-hello` address, or null. */
export const readHlHello = (data: unknown): string | null =>
  isRecord(data) && data.type === 'tk:hl-hello' && typeof data.url === 'string'
    ? data.url.slice(0, 2048)
    : null;

/** The selection a click makes: the element, named by the page it is on. */
export const selectedOf = (click: IHlClick, board: ISinopiaBoard | null): ISelectedNode => ({
  ...(board && click.tpl !== null ? { id: `${board.id}:${click.tpl}` } : {}),
  ...(board ? { page: board.id } : {}),
  ...(click.tpl !== null ? { tpl: click.tpl } : {}),
  ...(click.fp ? { fp: click.fp } : {}),
  ...(click.selector ? { selector: click.selector } : {}),
  field: click.field,
  tag: click.tag || 'div',
  ...(click.text ? { text: click.text } : {}),
  rect: click.rect,
  ...(Object.keys(click.hl).length > 0 ? { hl: click.hl } : {}),
});
