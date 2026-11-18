// «Документы» in the chat's right column: the documents, boards and designs this chat and every chat it
// delegated (down the tree) made or changed — the person works in the root chat, so what the helpers wrote is here
// too. Files are not: only pages of the catalogue and designs. A row opens the thing in the pane. Read off
// GET …/made, asked when the chat changes and every half minute while the column is drawn.
import { useEffect, useState } from 'react';
import { loadMade } from '@/next/commands/pages';
import type { ConvId, MachineId, MadeItem, TurnOutput } from '@/next/protocol/v2';
import { hasFeature } from '@/next/stores/machines';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

/** How often the list is asked again while it is on screen. */
export const DOCS_EVERY_MS = 30_000;
/** At most this many rows: the newest. */
export const DOCS_MAX = 8;

const DOC_KINDS = { doc: 'artifact', board: 'artifact', design: 'design' } as const;
type TDocKind = keyof typeof DOC_KINDS;
const ICON: Record<TDocKind, string> = { doc: 'file-text', board: 'workflow', design: 'palette' };

const isDoc = (item: MadeItem): item is MadeItem & { kind: TDocKind } => item.kind in DOC_KINDS;

/** The documents of the list: each once by its latest change, the newest first. */
export const docsOf = (items: readonly MadeItem[]): (MadeItem & { kind: TDocKind })[] => {
  const latest = new Map<string, MadeItem & { kind: TDocKind }>();
  for (const item of items) {
    if (!isDoc(item)) continue;
    const key = `${item.kind}:${item.ref}`;
    const seen = latest.get(key);
    if (!seen || item.at > seen.at) latest.set(key, item);
  }
  return [...latest.values()].sort((a, b) => b.at - a.at).slice(0, DOCS_MAX);
};

const useChatDocs = (machine: MachineId, conv: ConvId): readonly MadeItem[] => {
  const [list, setList] = useState<{ conv: ConvId; items: readonly MadeItem[] }>({ conv, items: [] });
  const announced = hasFeature(machine, 'made');
  useEffect(() => {
    if (!announced) return;
    let live = true;
    const ask = () =>
      void loadMade(machine, conv).then((outcome) => {
        if (live && 'ok' in outcome) setList({ conv, items: outcome.value.items });
      });
    ask();
    const timer = window.setInterval(ask, DOCS_EVERY_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [machine, conv, announced]);
  return list.conv === conv ? list.items : [];
};

const hhmm = (ms: number): string => {
  const d = new Date(ms);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
};

interface IChatDocsProps {
  machine: MachineId;
  conv: ConvId;
  onOpen: (output: TurnOutput) => void;
}

const ChatDocs = ({ machine, conv, onOpen }: IChatDocsProps) => {
  const docs = docsOf(useChatDocs(machine, conv));
  if (docs.length === 0) return null;
  return (
    <section className="hx-chat-docs" aria-label={t('next.chatDocs.title')}>
      <h3 className="hx-chat-docs__title">{t('next.chatDocs.title')}</h3>
      <ul className="hx-chat-docs__list">
        {docs.map((doc) => (
          <li key={`${doc.kind}:${doc.ref}`}>
            <button
              type="button"
              className="hx-chat-docs__row"
              onClick={() =>
                onOpen({ kind: DOC_KINDS[doc.kind], op: doc.op, ref: doc.ref, title: doc.title })
              }
            >
              <Icon name={ICON[doc.kind]} size="sm" className="hx-chat-docs__ic" />
              <span className="hx-chat-docs__name">{doc.title || doc.ref}</span>
              <span className="hx-chat-docs__meta">
                {t(doc.op === 'created' ? 'next.chatDocs.created' : 'next.chatDocs.changed')} · {hhmm(doc.at)}
                {doc.from?.kind === 'subchat' && doc.from.title ? ` · ${doc.from.title}` : ''}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
};

export default ChatDocs;
