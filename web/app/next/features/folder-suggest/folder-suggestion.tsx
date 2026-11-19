// «Переместить в папку?»: after a few prompts of a chat that has no folder, the machine's model picks the
// fitting one of the person's folders and the chat offers it in a small notice floating under the header (it lies over
// the feed and shifts nothing): move, or leave it. The other folders are folded under it; a move ends in a short
// «moved» line, and the notice leaves with the same soft exit it came with.
import { useEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
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
/** The notice's exit, ms: the same as .hx-foldersug's in folder-suggest.css. */
const EXIT_MS = 240;
/** How long «moved» stays before the notice leaves, ms. */
const DONE_MS = 2200;

interface IOffer {
  chat: string;
  project: ISidebarProject;
  /** `ask`: the offer; `done`: the chat moved, the notice says so and leaves. */
  phase: 'ask' | 'done';
}

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
  const [offer, setOffer] = useState<IOffer | null>(null);
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
      if (project) setOffer({ chat: ids, project, phase: 'ask' });
    });
    // The page asks once per step of the chat (3 and 6 prompts): `count` is the step's key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eligible, chatId, count]);

  // The moved chat has a folder now: the offer's own conditions end, the confirmation stands on its own phase.
  const visible =
    offer !== null &&
    offer.chat === chatId &&
    chat !== null &&
    (offer.phase === 'done' || busy || chat.projectId === null)
      ? offer
      : null;
  const { value: shown, state } = usePresence(visible, EXIT_MS);
  const donePhase = offer?.phase === 'done';
  useEffect(() => {
    if (!donePhase) return;
    const timer = setTimeout(() => setOffer(null), DONE_MS);
    return () => clearTimeout(timer);
  }, [donePhase]);

  if (!shown) return null;
  const { project } = shown;
  const closing = state === 'closing';
  const move = (projectId: string) => {
    if (chat === null) return;
    const target = (projects ?? []).find((p) => p.id === projectId) ?? project;
    setBusy(true);
    const moving = onMove(chat.id, projectId);
    // Moved: say so for a moment. Failed: the notice goes, as before.
    moving.then(
      () => setOffer({ chat: chat.id, project: target, phase: 'done' }),
      () => setOffer(null),
    );
    void moving.finally(() => {
      setBusy(false);
      setOpen(false);
    });
  };
  if (shown.phase === 'done') {
    return (
      <div className="hx-foldersug" role="status" data-state={state} data-done>
        <div className="hx-foldersug__card">
          <div className="hx-foldersug__row hx-foldersug__row--done">
            <Icon name="circle-check" size="sm" className="hx-foldersug__ok" />
            <span className="hx-foldersug__txt">
              {t('next.folderSuggest.done', { project: project.name })}
            </span>
          </div>
        </div>
      </div>
    );
  }
  const unfolded = open && !closing;
  return (
    <div className="hx-foldersug" role="status" data-state={state} data-open={unfolded || undefined}>
      <div className="hx-foldersug__card">
        <div className="hx-foldersug__row">
          <Icon name="folder-input" size="sm" className="hx-foldersug__icon" />
          <button
            type="button"
            className="hx-foldersug__head"
            aria-expanded={unfolded}
            aria-label={unfolded ? t('next.folderSuggest.fold') : t('next.folderSuggest.unfold')}
            onClick={() => setOpen((now) => !now)}
          >
            <span className="hx-foldersug__txt">
              {t('next.folderSuggest.ask', { project: project.name })}
            </span>
            <Icon name="chevron-down" size="sm" className="hx-foldersug__chev" />
          </button>
          <button
            type="button"
            className="hx-foldersug__yes"
            disabled={busy || closing}
            onClick={() => move(project.id)}
          >
            {t('next.folderSuggest.yes')}
          </button>
          <button
            type="button"
            className="hx-foldersug__no"
            aria-label={t('next.folderSuggest.no')}
            title={t('next.folderSuggest.no')}
            onClick={() => {
              keepAsked(chat?.id ?? shown.chat, { at: Number.MAX_SAFE_INTEGER, no: true });
              setOffer(null);
            }}
          >
            <Icon name="x" size="sm" />
          </button>
        </div>
        {/* The other folders stay mounted and fold by height (a grid row 0fr to 1fr); inert while folded. */}
        <div className="hx-foldersug__more" inert={!unfolded || undefined}>
          <div className="hx-foldersug__clip">
            <p className="hx-foldersug__why">{t('next.folderSuggest.why')}</p>
            <div className="hx-foldersug__pick" role="group" aria-label={t('next.folderSuggest.other')}>
              {(projects ?? []).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="hx-foldersug__chip"
                  data-suggested={item.id === project.id || undefined}
                  disabled={busy || closing}
                  onClick={() => move(item.id)}
                >
                  <Icon name="folder" size="sm" />
                  {item.name}
                </button>
              ))}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default FolderSuggestion;
