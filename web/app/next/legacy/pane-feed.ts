// What the runtime gathers for the right pane (the `pane` of a conversation's channel) in the shapes Legacy's pane
// contents read: its items as the timeline entries they stand for, with only the fields those contents look at, and
// its links as Legacy's. Part of the bridge: it goes with it when Legacy goes.
import { artifactGroupOf, type IArtifactLink } from '@/lib/hl-artifacts';
import type { LinkKind } from '@/lib/presented-link';
import type { ITimelineEntry } from '@/types/timeline';
import type { PaneItem, PaneLink } from '@/next/protocol/v2';

const entryOf = (item: PaneItem): ITimelineEntry | null => {
  const base = { id: item.id, timestamp: item.at };
  switch (item.k) {
    case 'tool':
      return {
        ...base,
        type: 'tool-call',
        toolUseId: item.use ?? '',
        toolName: item.tool ?? '',
        summary: item.text ?? '',
        status: 'success',
        ...(item.path ? { filePath: item.path } : {}),
        ...(item.peer ? { peerMessage: item.peer } : {}),
        ...(item.files ? { sentFiles: { paths: item.files } } : {}),
      };
    case 'result':
      return { ...base, type: 'tool-result', toolUseId: item.use ?? '', isError: false, summary: '' };
    case 'notice':
      return { ...base, type: 'task-notification', taskId: '', status: '', summary: '' };
    case 'user':
      return {
        ...base,
        type: 'user-message',
        text: item.text ?? '',
        ...(item.images ? { images: item.images } : {}),
      };
    case 'answer':
      return { ...base, type: 'assistant-message', markdown: item.text ?? '' };
    case 'patch':
      return {
        ...base,
        type: 'patch-apply',
        callId: '',
        files: (item.files ?? []).map((path) => ({ path })),
        status: 'success',
      };
    case 'exec':
      return {
        ...base,
        type: 'exec-command-stream',
        command: item.text ?? '',
        status: 'success',
        ...(item.cwd ? { cwd: item.cwd } : {}),
      };
    default:
      return null;
  }
};

// One entry per item for as long as the item lives: the contents remember what they found in an entry by the entry.
const entries = new WeakMap<PaneItem, ITimelineEntry | null>();

/** The pane's items as timeline entries, oldest first; an item of a kind this page does not know is left out. */
export const paneEntries = (items: readonly PaneItem[]): ITimelineEntry[] => {
  const out: ITimelineEntry[] = [];
  for (const item of items) {
    let entry = entries.get(item);
    if (entry === undefined) entries.set(item, (entry = entryOf(item)));
    if (entry) out.push(entry);
  }
  return out;
};

/** The pane's links as Legacy's, in the runtime's order (newest first). */
export const paneLinks = (links: readonly PaneLink[]): IArtifactLink[] =>
  links.map((link) => {
    const kind = link.kind as LinkKind;
    return {
      url: link.url,
      label: link.label,
      timestamp: link.at,
      count: link.count,
      kind,
      group: artifactGroupOf(kind),
    };
  });
