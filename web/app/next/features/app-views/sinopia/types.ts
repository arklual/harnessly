// What an app puts into Sinopia, as the runtime lists it (internal/apps/sinopia.go) and what the editor asks it of a
// document (cmd/runtime/routes_apps_sinopia.go).

export interface ISinopiaPanel {
  id: string;
  view: string;
  title?: string;
  kinds?: string[];
}

export interface ISinopiaSelectionItem {
  id: string;
  title: string;
  /** `<server>.<tool>` of the app's own MCP server. */
  tool?: string;
  /** A task for the agent of the chat linked to the document. */
  agent?: string;
  kinds?: string[];
}

export interface ISinopiaBindings {
  schema?: string;
  /** The view that opens when a bound element is clicked. */
  inspector: string;
  lockUnbound?: boolean;
}

export interface ISinopiaCheck {
  id: string;
  title?: string;
  on?: string[];
  blockShare?: string;
  kinds?: string[];
}

export interface ISinopia {
  panels?: ISinopiaPanel[];
  selection?: ISinopiaSelectionItem[];
  bindings?: ISinopiaBindings;
  checks?: ISinopiaCheck[];
  templates?: string[];
}

export const MARK_LEVELS = ['error', 'warning', 'info', 'comment'] as const;
export type TMarkLevel = (typeof MARK_LEVELS)[number];

/** A note on an element of a board: a finding of a check, an app's mark or a comment. */
export interface ISinopiaMark {
  /** `<page>:<number>`: the page's id and the element's `data-dc-tpl`. */
  node: string;
  level: TMarkLevel;
  note: string;
  source: string;
  rule?: string;
}

export interface ISinopiaFinding {
  node: string;
  level: string;
  message: string;
  rule?: string;
}

export interface ISinopiaCheckResult {
  app: string;
  appName: string;
  check: string;
  title?: string;
  findings: ISinopiaFinding[];
  /** The check keeps the document from being shared as it is. */
  blocks: boolean;
  /** Why the check could not run. */
  failed?: string;
}

export interface ISinopiaBoard {
  id: string;
  title: string;
  /** The board's file in the project: the path its frame is at. */
  file: string;
}

/** What the runtime says of an open document (POST /apps/sinopia/state). */
export interface ISinopiaState {
  kind: string;
  rev: number;
  boards: ISinopiaBoard[];
  /** The ids of the apps on in the document. */
  apps: string[];
  lockUnbound: boolean;
  marks: ISinopiaMark[];
  results: ISinopiaCheckResult[];
}

/** A picked or clicked element, as the runtime keeps a selection (apps.SelectedNode). */
export interface ISelectedNode {
  id?: string;
  page?: string;
  tpl?: number;
  fp?: string;
  selector?: string;
  field?: string;
  tag: string;
  text?: string;
  rect?: { x: number; y: number; width: number; height: number };
  hl?: Record<string, string>;
}
