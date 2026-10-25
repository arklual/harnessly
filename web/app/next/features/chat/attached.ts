// The places a message points at (a design, a document, a board, a file of the project, a merge request, a task) as
// the runtime writes them under the person's words (internal/attach): one `<attached>` block at the end of the text, a
// line per place led by `<ref kind=".." id=".." title=".."/>`. The page never shows the block: it reads it back into
// chips, the words stay the person's own.
import { REF_KINDS, type MessageRef, type RefKind } from '@/next/protocol/v2';

/** The block at the very end of a text, after a blank line or alone. */
const BLOCK_RE = /(?:^|\n\n)<attached>\n([\s\S]*?)\n<\/attached>\s*$/;
const REF_RE = /^<ref kind="([a-z]+)" id="([^"]*)" title="([^"]*)"\/>/;

const unescape = (s: string): string =>
  s.replace(/&(quot|lt|gt|amp);/g, (_, name: string) => {
    if (name === 'quot') return '"';
    if (name === 'lt') return '<';
    if (name === 'gt') return '>';
    return '&';
  });

const isRefKind = (kind: string): kind is RefKind => (REF_KINDS as readonly string[]).includes(kind);

export interface IAttachedRead {
  /** The person's words without the block. */
  text: string;
  /** The places the block names, in order; empty when there is none. */
  refs: MessageRef[];
}

const NONE: MessageRef[] = [];

/** The words and the places of a text the runtime wrote; a text without the block is the words alone. */
export const readAttached = (text: string): IAttachedRead => {
  const m = BLOCK_RE.exec(text);
  if (!m) return { text, refs: NONE };
  const refs = m[1].split('\n').flatMap((line): MessageRef[] => {
    const tag = REF_RE.exec(line);
    if (!tag || !isRefKind(tag[1])) return [];
    const kind = tag[1];
    const id = unescape(tag[2]);
    // The line's own words are the agent's; the id is where the page opens it again (a design has no target).
    return [{ kind, id, title: unescape(tag[3]), target: kind === 'design' ? '' : id }];
  });
  return { text: text.slice(0, m.index).trimEnd(), refs };
};

/**
 * What a bubble or a queued row shows: the words without the block, and the chips — the message's own references
 * while the runtime has not written the block yet, the block's once it has.
 */
export const attachedOf = (text: string, refs: readonly MessageRef[] | undefined): IAttachedRead => {
  const read = readAttached(text);
  if (read.refs.length > 0 || !refs || refs.length === 0) return read;
  return { text: read.text, refs: [...refs] };
};

/** The icon of a place's chip. */
export const REF_ICON: Record<RefKind, string> = {
  design: 'palette',
  doc: 'file-text',
  board: 'presentation',
  file: 'file-code',
  mr: 'git-pull-request',
  task: 'list-checks',
} as const;

/** The key that tells one place from another in a field. */
export const refKey = (ref: Pick<MessageRef, 'kind' | 'id'>): string => `${ref.kind}:${ref.id}`;
