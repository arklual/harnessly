/** Shared by the gateway API and the browser. */

export const DOC_THREAD_STATUS = { open: 'open', resolved: 'resolved' } as const;
export type TDocThreadStatus = (typeof DOC_THREAD_STATUS)[keyof typeof DOC_THREAD_STATUS];

/** `guest`: somebody without a sign-in who wrote through a link with the right to comment; `email` is empty. */
export const DOC_AUTHOR_KIND = { person: 'person', agent: 'agent', guest: 'guest' } as const;
export type TDocAuthorKind = (typeof DOC_AUTHOR_KIND)[keyof typeof DOC_AUTHOR_KIND];

/** W3C TextQuoteSelector plus the nearest heading, so a thread survives edits around its words. */
export interface IDocAnchor {
  /** For a board thread: the label of the anchored element, or «Canvas». */
  quote: string;
  prefix: string;
  suffix: string;
  headingId?: string;
  headingText?: string;
  /** Board threads only: the Excalidraw element the pin rides on, and the pin's scene position. */
  elementId?: string;
  x?: number;
  y?: number;
}

export interface IDocAuthor {
  kind: TDocAuthorKind;
  email: string;
  name: string;
}

export interface IDocComment {
  id: string;
  threadId: string;
  author: IDocAuthor;
  body: string;
  createdAt: string;
  /** Agent-bound delivery of a person's comment: null until the conversation received it. */
  deliveredAt: string | null;
  deliveryError: string | null;
}

export interface IDocThread {
  id: string;
  /** The thread id in the boards service for a mirrored board thread, else null. */
  externalId: string | null;
  slug: string;
  anchor: IDocAnchor;
  status: TDocThreadStatus;
  createdBy: IDocAuthor;
  createdAt: string;
  resolvedAt: string | null;
  comments: IDocComment[];
}

export interface IDocSummary {
  slug: string;
  /** 'artifact' (a document of the documents service, /a/) or 'board' (the boards service, /b/). */
  kind: 'artifact' | 'board';
  title: string;
  /** A board: its address on the boards service with the key. An artifact: its page in the viewer — a closed document has no public address, `IDocAccess` is the one that does. */
  url: string;
  /** False for a document no conversation published: comments are kept, but no agent receives them. */
  bound: boolean;
  /** Null while the document is not bound to a conversation. */
  owner: IDocAuthor | null;
  machineId: string | null;
  sourcePath: string | null;
  openThreads: number;
  unread: number;
  lastActivityAt: string;
}

/** A person offered by the @ list: the id is the email a mention carries. */
export interface IDocPerson {
  id: string;
  name: string;
  initial: string;
}

export interface IDocPeopleResponse {
  people: IDocPerson[];
}

export interface IDocListResponse {
  docs: IDocSummary[];
}

/** A document's public link: `publicUrl` opens it without login, read-only and without comments, while `public` is true. */
export interface IDocAccess {
  public: boolean;
  publicUrl: string | null;
}

/**
 * How the current version is edited in place: `html` block by block in its own page (its markup and widgets stay),
 * `markdown` as its Markdown text when the version has no page of its own.
 */
export const DOC_EDIT_MODE = { html: 'html', markdown: 'markdown' } as const;
export type TDocEditMode = (typeof DOC_EDIT_MODE)[keyof typeof DOC_EDIT_MODE];

/** What editing in place the current version allows; `markdown` is set in the `markdown` mode only. */
export interface IDocSource {
  mode: TDocEditMode | null;
  markdown: string | null;
  /** The number of the current version: the base an edit is saved against. */
  version: number;
  canEdit: boolean;
}

export interface IDocDetailResponse {
  doc: IDocSummary;
  threads: IDocThread[];
}

/** One version of a document: every publish of a slug, a commit in a git-backed portal or a version in the artifacts service. */
export interface IDocVersion {
  /** 1 for the first publish; the current version has the largest number. */
  n: number;
  /** The artifacts service's opaque id; absent from a git-backed portal, whose id is `sha`. */
  id?: string;
  /** The commit hash; the artifacts service repeats its id here. */
  sha: string;
  createdAt: string;
  /** Who published it, as the portal records it (an email, a runtime name); null if unknown. */
  author: string | null;
  note: string | null;
  current: boolean;
}

export interface IDocVersionsResponse {
  /** Newest first. */
  versions: IDocVersion[];
}

export const DOC_DIFF_KIND = { same: 'same', added: 'added', removed: 'removed' } as const;
export type TDocDiffKind = (typeof DOC_DIFF_KIND)[keyof typeof DOC_DIFF_KIND];

export interface IDocDiffBlock {
  kind: TDocDiffKind;
  text: string;
}

/** The visible text of two versions compared block by block. */
export interface IDocDiffResponse {
  from: string;
  to: string;
  blocks: IDocDiffBlock[];
  stats: { added: number; removed: number };
}

/** postMessage protocol between the viewer and the sandboxed document frame. Rects are in the frame's viewport. */
export type TDocFrameMessage =
  | {
      type: 'doc:selection';
      anchor: IDocAnchor;
      rect: IDocRect;
      /** The last line of the selection: the comment button sits by its end. */ end: IDocRect;
    }
  | { type: 'doc:selection-cleared' }
  /** Positions of the thread marks; sent again on scroll and resize. null = the quote is gone. */
  | {
      type: 'doc:anchors';
      positions: Record<string, IDocRect | null>;
      /** Right edge of the page's text column, in the frame's viewport. */ columnRight: number;
    }
  | { type: 'doc:open'; threadId: string }
  /** A press or Esc inside the document: cancels an open composer. */
  | { type: 'doc:dismiss' }
  /** Ctrl/Cmd+Alt+M inside the document. */
  | { type: 'doc:comment-key' }
  /**
   * A double click on a block. `index`: the block's number in the page when it can be edited in place, null when it is
   * markup or a widget. `ordinal`/`total`: its place among the page's blocks of the same kind, to find it in Markdown.
   */
  | {
      type: 'doc:edit-block';
      text: string;
      tag: string;
      rect: IDocRect;
      index: number | null;
      ordinal: number;
      total: number;
    }
  /** The block being edited in place was finished (Enter): its new content and its text before the edit. */
  | { type: 'doc:edit-commit'; index: number; html: string; before: string; changed: boolean }
  /** Esc in the block being edited. */
  | { type: 'doc:edit-cancel' };

export type TDocParentMessage =
  | { type: 'doc:threads'; threads: { id: string; anchor: IDocAnchor; status: TDocThreadStatus }[] }
  | { type: 'doc:focus'; threadId: string | null }
  /** The place a comment is being written on stays highlighted while the selection itself is gone. */
  | { type: 'doc:draft'; anchor: IDocAnchor }
  | { type: 'doc:draft-clear' }
  /** Scroll to this passage of the page and flash it (a changed block of a comparison). */
  | { type: 'doc:find'; text: string }
  /** Make block `index` editable in place, with the caret in it. */
  | { type: 'doc:edit-start'; index: number }
  /** Finish the edit as if Enter was pressed (the viewer's «Сохранить»): the frame answers with `doc:edit-commit`. */
  | { type: 'doc:edit-finish' }
  /** Back to the page as published: the edit is dropped (cancelled or refused). */
  | { type: 'doc:edit-revert' };

export interface IDocRect {
  top: number;
  left: number;
  width: number;
  height: number;
}
