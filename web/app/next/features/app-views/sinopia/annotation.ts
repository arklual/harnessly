// The task an app's selection item sends to the chat linked to a document: the app's words, then the selection as an
// `<annotation>` block (the same block the design's notes write), one line per element. Whatever the board supplied is
// one line each and cannot close the block, so a board's words cannot forge a line or an instruction.
import type { ISelectedNode } from '@/next/features/app-views/sinopia/types';

const MAX_NODES = 20;
const MAX_TEXT = 120;
const MAX_SELECTOR = 512;
const MAX_ID = 128;
const MAX_TASK = 2000;

const oneLine = (text: string) =>
  text
    .replace(/\x1b/g, '␛')
    .replace(/[\u2028\u2029\u0085]/g, ' ')
    .replace(/[\x00-\x1f\x7f-\x9f]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
const clip = (text: string, max: number) => (text.length > max ? `${text.slice(0, max - 1)}…` : text);
const keepInside = (text: string) => text.replace(/<(\/?)(annotation)/gi, '<\\$1$2');
const escapeMarkup = (text: string) =>
  text.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c] ?? c);

/** The line of one element: its node id (what the Document API takes), what it is and says. */
const lineOf = (n: ISelectedNode): string => {
  const parts = [`node: ${n.id ? clip(oneLine(n.id), MAX_ID) : '—'}`];
  if (n.field) parts.push(`field: ${clip(oneLine(n.field), MAX_ID)}`);
  if (n.selector) parts.push(`selector: ${clip(oneLine(n.selector), MAX_SELECTOR)}`);
  const text = n.text ? clip(oneLine(n.text), MAX_TEXT) : '';
  return keepInside(`${parts.join(' · ')} · <${oneLine(n.tag).slice(0, 32)}>${text ? ` «${text}»` : ''}`);
};

export interface ISelectionTask {
  /** The document's id. */
  document: string;
  /** The app's real name. */
  app: string;
  /** What the manifest's item asks of the agent. */
  task: string;
  nodes: readonly ISelectedNode[];
}

/** The message to the chat: `[App] task`, then the annotation of the selection. */
export const selectionMessage = ({ document, app, task, nodes }: ISelectionTask): string => {
  const name = oneLine(app.replace(/[[\]]+/g, ' '));
  const shown = nodes.slice(0, MAX_NODES);
  const attributes = [
    ['document', oneLine(document)],
    ['target', 'sinopia'],
  ]
    .map(([k, v]) => `${k}="${escapeMarkup(v)}"`)
    .join(' ');
  return [
    `[${name}] ${clip(oneLine(task), MAX_TASK)}`,
    '',
    `<annotation ${attributes}>`,
    ...shown.map(lineOf),
    ...(nodes.length > shown.length ? [`… and ${nodes.length - shown.length} more`] : []),
    '</annotation>',
  ].join('\n');
};

/** The note a view's `comment` sends the chat: the app's name, the comment, the element it is on. */
export const commentMessage = (
  document: string,
  app: string,
  node: ISelectedNode | null,
  nodeId: string,
  text: string,
) =>
  selectionMessage({
    document,
    app,
    task: text,
    nodes: [node ?? { id: nodeId, tag: 'node' }],
  });
