// «Переместить в папку?»: after a few prompts of a chat that has no folder, the machine's model picks the
// fitting one of the person's folders and the chat offers it in one line under the header: move, or leave it.
import { useEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import {
  askFolder,
  keepAsked,
  nextAsk,
  readAsked,
  type IFolderInfo,
} from '@/next/features/folder-suggest/ask';
import { useFolderPrefs } from '@/next/features/sidebar/folder-prefs';
import type { ISidebarChat, ISidebarProject } from '@/next/features/sidebar/types';
import { entryStore, key, useFeedRows } from '@/next/stores/conversation';
import type { ConvId, MachineId } from '@/next/protocol/v2';

const TITLES = 5;

export interface IFolderSuggestionProps {
  chat: ISidebarChat | null;
  machine: MachineId | null;
  projects: readonly ISidebarProject[] | null;
  chats: readonly ISidebarChat[] | null;
  onMove: (chatId: string, projectId: string) => Promise<void>;
}

/** The person's prompts of the chat that the page has, oldest first. */
const promptsOf = (conv: ConvId, rows: readonly { k: string; id: string }[]): string[] => {
  const out: string[] = [];
  for (const row of rows) {
    if (row.k !== 'turn') continue;
    const entry = entryStore.get(key(conv, row.id));
    if (entry && entry.t === 'user' && entry.text.trim() !== '') out.push(entry.text);
  }
  return out;
};

const FolderSuggestion = ({ chat, machine, projects, chats, onMove }: IFolderSuggestionProps) => {
  const prefs = useFolderPrefs();
  const conv: ConvId | null = chat !== null && chat.id.startsWith('tab-') ? (chat.id as ConvId) : null;
  const rows = useFeedRows(conv);
  const [offer, setOffer] = useState<{ chat: string; project: ISidebarProject } | null>(null);
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(false);
  const asking = useRef<string | null>(null);

  const chatId = chat?.id ?? null;
  const eligible =
    prefs.suggest &&
    chat !== null &&
    conv !== null &&
    chat.projectId === null &&
    chat.from === undefined &&
    machine !== null &&
    projects !== null &&
    projects.length > 0 &&
    rows !== null;
  const count = conv !== null && eligible && rows ? promptsOf(conv, rows).length : 0;

  useEffect(() => {
    if (!eligible || !chat || !conv || !machine || !projects || !rows) return;
    const want = nextAsk(readAsked(chat.id));
    if (want === null || count < want || asking.current === chat.id) return;
    asking.current = chat.id;
    const folders: IFolderInfo[] = projects.map((project) => ({
      id: project.id,
      name: project.name,
      titles: (chats ?? [])
        .filter((c) => c.projectId === project.id && c.id !== chat.id)
        .slice(0, TITLES)
        .map((c) => c.title),
    }));
    const ids = chat.id;
    void askFolder(machine, { chat: ids, prompts: promptsOf(conv, rows), folders }).then((id) => {
      asking.current = null;
      keepAsked(ids, { at: count, no: false });
      const project = id === null ? null : (projects.find((p) => p.id === id) ?? null);
      if (project) setOffer({ chat: ids, project });
    });
    // The page asks once per step of the chat (3 and 6 prompts): `count` is the step's key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eligible, chatId, count]);

  if (!offer || offer.chat !== chatId || chat === null || chat.projectId !== null) return null;
  const { project } = offer;
  const move = (projectId: string) => {
    setBusy(true);
    void onMove(chat.id, projectId).finally(() => {
      setBusy(false);
      setOpen(false);
      setOffer(null);
    });
  };
  return (
    <div className="hx-foldersug" role="status" data-open={open || undefined}>
      <div className="hx-foldersug__row">
        <button
          type="button"
          className="hx-foldersug__head"
          aria-expanded={open}
          aria-label={open ? t('next.folderSuggest.fold') : t('next.folderSuggest.unfold')}
          onClick={() => setOpen((now) => !now)}
        >
          <Icon name="folder-input" size="sm" />
          <span className="hx-foldersug__txt">{t('next.folderSuggest.ask', { project: project.name })}</span>
          <Icon name="chevron-down" size="sm" className="hx-foldersug__chev" />
        </button>
        <button type="button" className="hx-foldersug__yes" disabled={busy} onClick={() => move(project.id)}>
          {t('next.folderSuggest.yes')}
        </button>
        <button
          type="button"
          className="hx-foldersug__no"
          aria-label={t('next.folderSuggest.no')}
          title={t('next.folderSuggest.no')}
          onClick={() => {
            keepAsked(chat.id, { at: Number.MAX_SAFE_INTEGER, no: true });
            setOffer(null);
          }}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {open && (
        <div className="hx-foldersug__more">
          <p className="hx-foldersug__why">{t('next.folderSuggest.why')}</p>
          <div className="hx-foldersug__pick" role="group" aria-label={t('next.folderSuggest.other')}>
            {(projects ?? []).map((item) => (
              <button
                key={item.id}
                type="button"
                className="hx-foldersug__chip"
                data-suggested={item.id === project.id || undefined}
                disabled={busy}
                onClick={() => move(item.id)}
              >
                <Icon name="folder" size="sm" />
                {item.name}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

export default FolderSuggestion;
