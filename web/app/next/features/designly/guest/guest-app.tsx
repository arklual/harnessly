// The page of a guest link: the design is looked at the way its owner looks at it (SiteView with the owner's view), without
// chats, editing and sharing. A guest with the right to comment picks a block and leaves a comment where the owner's card
// would take a note; the comment goes to the link's gateway and comes back as a pin on the block.
import { useEffect, useRef, useState } from 'react';
import SiteView, { type ISiteStage, type ISiteTab } from '@/next/features/designly/site-view';
import { PortalContext } from '@/next/ui/portal';
import { ToastHost, toast } from '@/next/ui/toast';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { NO_EDIT } from '@/next/features/designly/element-style';
import { anchorItems } from '@/next/features/designly/note-anchor';
import { usePageDraft } from '@/next/features/designly/page-drafts';
import {
  NOTE_OUT_MS,
  NotePins,
  NotePopover,
  NoteStrip,
  type INote,
} from '@/next/features/designly/site-notes';
import type { TTweakValue } from '@/next/features/designly/page-bridge';
import { boardUrl, sharedView, type IGuestConfig } from '@/next/features/designly/guest/guest-config';
import {
  anchorFor,
  cleanState,
  fetchComments,
  keepWho,
  postComment,
  readWho,
  type IGuestWho,
  type IGuestComment,
} from '@/next/features/designly/guest/guest-comments';
import GuestNameDialog from '@/next/features/designly/guest/guest-name-dialog';
import { t } from '@/next/i18n';

const SEED_TABS = 6;
const COMMENTS_MS = 20_000;
let tabSeq = 0;

const newTab = (path: string): ISiteTab => ({
  id: `g${Date.now().toString(36)}${(++tabSeq).toString(36)}`,
  hist: [path],
  hi: 0,
});

/** The first pages are the tabs, as for the owner; the page the owner shared is the one on screen. */
const startTabs = (config: IGuestConfig, page: string | null): ISiteTab[] => {
  const paths = config.boards.slice(0, SEED_TABS).map((b) => b.path);
  if (page && !paths.includes(page) && config.boards.some((b) => b.path === page)) paths.push(page);
  return paths.map(newTab);
};

const none = (): void => {};

/** A comment as the note the pins and the strip of the owner's view know: its words with the guest's name. */
const noteOf = (c: IGuestComment): INote | null => {
  const a = c.anchor;
  if (!a) return null;
  return {
    id: `c${c.id}`,
    picked: {
      id: a.dataId,
      look: null,
      selector: a.selector,
      tag: a.tag,
      text: a.label,
      rect: a.rect,
      component: null,
      html: '',
    },
    page: c.board,
    text: c.name ? `${c.name}: ${c.text}` : c.text,
    at: { x: 0, y: 0, right: false },
    style: NO_EDIT,
  };
};

const useComments = (config: IGuestConfig) => {
  const [comments, setComments] = useState<IGuestComment[]>([]);
  const on = config.rights === 'comment' && config.commentsUrl !== '';
  useEffect(() => {
    if (!on) return;
    const abort = new AbortController();
    const read = () =>
      void fetchComments(config.commentsUrl, abort.signal).then((list) => {
        if (list) setComments(list);
      });
    read();
    const timer = window.setInterval(read, COMMENTS_MS);
    return () => {
      abort.abort();
      window.clearInterval(timer);
    };
  }, [on, config.commentsUrl]);
  return [comments, setComments] as const;
};

interface ICommentStage {
  stage: ISiteStage;
  /** Turns picking on: the first thing a guest does after saying the name. */
  startPicking: () => void;
}

const useCommentStage = (
  config: IGuestConfig,
  pageUrl: string,
  who: IGuestWho,
  askName: () => void,
): ICommentStage => {
  const [comments, setComments] = useComments(config);
  const state = useRef<Record<string, TTweakValue>>({});
  const notes = comments.flatMap((c) => noteOf(c) ?? []);
  const pageNotes = notes.filter((n) => n.page === pageUrl);
  const [draft, setDraft] = usePageDraft(pageUrl);
  const card = usePresence(draft, NOTE_OUT_MS);
  const shown = card.value;
  const [picking, setPicking] = useState(false);
  const [bridged, setBridged] = useState(false);

  const submit = async (words: string): Promise<boolean> => {
    if (!shown) return false;
    const name = who.name.trim();
    try {
      const taken = await postComment(config.commentsUrl, {
        who: { ...who, name },
        text: words,
        board: pageUrl,
        anchor: anchorFor(shown.picked, state.current),
      });
      setComments((list) => [...list, taken]);
      toast(t('next.guest.sent'), { tone: 'success' });
      setDraft(null);
      return true;
    } catch {
      toast(t('next.guest.failed'), { tone: 'error' });
      return false;
    }
  };

  const stage: ISiteStage = {
    mode: 'island',
    onMode: none,
    min: true,
    run: 'idle',
    float: null,
    dock: 'left',
    chatW: 0,
    chat: null,
    island: null,
    pick: bridged && draft === null && picking,
    picking: bridged ? picking : null,
    onPick: () => {
      if (!picking && who.name === '') askName();
      else setPicking((on) => !on);
    },
    onPicked: (picked, at) => {
      setPicking(false);
      setDraft({ picked, at, text: '', style: NO_EDIT });
    },
    onControls: (controls) => {
      state.current = cleanState(controls);
    },
    onPickEnd: () => setPicking(false),
    onBridge: setBridged,
    styles: [],
    anchors: anchorItems(pageNotes, draft),
    onStyled: none,
    sview: false,
    onSview: none,
    onAttach: none,
    pins: (
      <NotePins
        notes={pageNotes}
        draft={draft}
        onOpen={() => {
          // The comment is read from its pin's title; opening it again is for the owner's notes.
        }}
      />
    ),
    overlay: (
      <>
        <div className="hx-guest__strip">
          <NoteStrip notes={notes} readOnly onEdit={none} onRemove={none} onClear={none} />
        </div>
        {shown && (
          <NotePopover
            key={`${shown.picked.selector}@${shown.at.x},${shown.at.y}`}
            n={notes.length + 1}
            draft={shown}
            text={shown.text}
            onText={(text) => setDraft({ ...shown, text })}
            mic={null}
            needsText
            onOk={none}
            onCancel={() => setDraft(null)}
            onDelete={() => setDraft(null)}
            onStyle={none}
            gone={false}
            hidden={card.state === PRESENCE.closing}
            guest={{ submit }}
          />
        )}
      </>
    ),
  };
  return { stage, startPicking: () => setPicking(true) };
};

const GuestBody = ({ config }: { config: IGuestConfig }) => {
  const view = sharedView(config.view);
  const sharedPage = typeof config.view?.page === 'string' ? config.view.page : null;
  const [tabs, setTabs] = useState<ISiteTab[]>(() => startTabs(config, sharedPage));
  const [tab, setTab] = useState<string>(() => {
    const at = tabs.findIndex((x) => x.hist[x.hi] === sharedPage);
    return (tabs[Math.max(0, at)] ?? { id: '' }).id;
  });
  const [reloadKey, setReloadKey] = useState(0);
  const current = tabs.find((x) => x.id === tab) ?? tabs[0];
  const path = current ? current.hist[current.hi] : null;
  const src = path === null ? null : boardUrl(config.filesBase, path);
  const [who, setWho] = useState(readWho);
  const [naming, setNaming] = useState(false);
  const { stage, startPicking } = useCommentStage(config, path ?? '', who, () => setNaming(true));
  const updateTab = (change: (x: ISiteTab) => ISiteTab) =>
    setTabs((list) => list.map((x) => (x.id === current?.id ? change(x) : x)));

  return (
    <>
      <SiteView
        title={config.title}
        pages={config.boards.map((b) => ({ path: b.path, name: b.title || b.path }))}
        tabs={tabs}
        tab={current?.id ?? ''}
        src={src}
        plainSrc={src}
        error={tabs.length === 0 ? t('next.guest.empty') : null}
        reloadKey={reloadKey}
        onPickTab={setTab}
        onCloseTab={(closing) => {
          const at = tabs.findIndex((x) => x.id === closing);
          const next = tabs.filter((x) => x.id !== closing);
          setTabs(next);
          if (closing === current?.id && next.length > 0) setTab(next[Math.max(0, at - 1)].id);
        }}
        onNewTab={(page) => {
          const added = newTab(page);
          setTabs((list) => [...list, added]);
          setTab(added.id);
        }}
        onNavigate={(page) =>
          updateTab((x) =>
            x.hist[x.hi] === page ? x : { ...x, hist: [...x.hist.slice(0, x.hi + 1), page], hi: x.hi + 1 },
          )
        }
        onHistory={(step) =>
          updateTab((x) => ({ ...x, hi: Math.max(0, Math.min(x.hist.length - 1, x.hi + step)) }))
        }
        onReload={() => setReloadKey((n) => n + 1)}
        onOpenOutside={none}
        onCopyLink={() => void navigator.clipboard?.writeText(location.href).catch(none)}
        onRetry={() => setReloadKey((n) => n + 1)}
        onOpenDrawer={none}
        onToggleMax={none}
        maxed={false}
        stage={config.rights === 'comment' ? stage : undefined}
        memoryKey={`guest:${config.token}`}
        viewSeed={view}
        guest={{
          pickLabel: t('next.guest.pick'),
          pickTip: t('next.guest.pickTip'),
          account:
            config.rights === 'comment' && who.name !== ''
              ? { label: t('next.guest.account', { name: who.name }), onOpen: () => setNaming(true) }
              : null,
        }}
      />
      {config.rights === 'comment' && (
        <GuestNameDialog
          key={naming ? 'open' : 'shut'}
          open={naming}
          name={who.name}
          onClose={() => setNaming(false)}
          onSave={(name) => {
            const next = { ...who, name };
            keepWho(next);
            setWho(next);
            setNaming(false);
            if (who.name === '') startPicking();
          }}
        />
      )}
    </>
  );
};

const GuestApp = ({ config }: { config: IGuestConfig | null }) => {
  const portal = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (config) document.title = config.title;
  }, [config]);
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-guest">
        {config ? (
          <main className="hx-main">
            <GuestBody config={config} />
          </main>
        ) : (
          <p className="hx-guest__empty">{t('next.guest.noLink')}</p>
        )}
        <div ref={portal} className="hx-portal" />
        <ToastHost />
      </div>
    </PortalContext>
  );
};

export default GuestApp;
