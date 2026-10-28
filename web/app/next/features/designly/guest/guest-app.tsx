// The page of a guest link: the design is looked at the way its owner looks at it (SiteView with the owner's view), without
// chats, editing and sharing. A guest with the right to comment picks a block and leaves a comment where the owner's card
// would take a note; the comment goes to the link's gateway and comes back as a pin on the block.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
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
  keepWho,
  postComment,
  readWho,
  type IGuestWho,
  type IGuestComment,
} from '@/next/features/designly/guest/guest-comments';
import { useComments } from '@/next/features/designly/guest/use-guest-comments';
import GuestDeck from '@/next/features/designly/guest/guest-deck';
import GuestNameDialog from '@/next/features/designly/guest/guest-name-dialog';
import GuestFull, { useTouch } from '@/next/features/designly/guest/guest-full';
import type { IPicked } from '@/next/features/designly/page-bridge';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

const SEED_TABS = 6;
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

interface ICommentStage {
  stage: ISiteStage;
  notes: INote[];
  /** After the first name: the block held before it opens its card, else picking turns on. */
  afterName: () => void;
}

interface IHeld {
  picked: IPicked;
  at: { x: number; y: number; right: boolean };
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
  // A block held before the guest said a name waits for the name.
  const held = useRef<IHeld | null>(null);

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
    hold: true,
    picking: bridged ? picking : null,
    onPick: () => {
      if (!picking && who.name === '') askName();
      else setPicking((on) => !on);
    },
    onPicked: (picked, at) => {
      setPicking(false);
      if (who.name === '') {
        held.current = { picked, at };
        askName();
        return;
      }
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
  const afterName = () => {
    const first = held.current;
    held.current = null;
    if (first) setDraft({ ...first, text: '', style: NO_EDIT });
    else setPicking(true);
  };
  return { stage, notes, afterName };
};

/** The full screen: off, on, or just left (the page settles back with its animation). */
export const FULL = { off: 'off', on: 'on', left: 'left' } as const;
type TFull = (typeof FULL)[keyof typeof FULL];

/** A key pressed while the guest writes is the field's. */
const typingIn = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

const askBrowserFull = () => {
  const root = document.documentElement;
  if (document.fullscreenElement || typeof root.requestFullscreen !== 'function') return;
  root.requestFullscreen().catch(none);
};
const leaveBrowserFull = () => {
  if (document.fullscreenElement && typeof document.exitFullscreen === 'function')
    document.exitFullscreen().catch(none);
};

const GuestBody = ({
  config,
  full,
  onFull,
}: {
  config: IGuestConfig;
  full: TFull;
  onFull: (next: TFull) => void;
}) => {
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
  const { stage, notes, afterName } = useCommentStage(config, path ?? '', who, () => setNaming(true));
  const commenting = config.rights === 'comment';
  const touch = useTouch();
  const isFull = full === FULL.on;
  const openOutside = src === null ? undefined : () => window.open(src, '_blank', 'noopener');

  const enter = () => {
    onFull(FULL.on);
    // A computer hides its own bars too; a phone keeps them (Safari has no full screen for a page) and gets the handle.
    if (!touch) askBrowserFull();
    if (touch) toast(t(commenting ? 'next.guest.holdHint' : 'next.guest.dragHint'));
  };
  const exit = () => {
    onFull(FULL.left);
    leaveBrowserFull();
  };
  // Esc of the browser's own full screen leaves it without a key for the page: the page leaves with it.
  const fullNow = useRef(isFull);
  useEffect(() => {
    fullNow.current = isFull;
  });
  useEffect(() => {
    const onChange = () => {
      if (!document.fullscreenElement && fullNow.current) onFull(FULL.left);
    };
    document.addEventListener('fullscreenchange', onChange);
    return () => document.removeEventListener('fullscreenchange', onChange);
  }, [onFull]);
  // F opens and closes the full screen, Esc leaves it, C picks a block; the board passes the same keys over itself.
  const onKey = useEffectEvent((event: KeyboardEvent) => {
    if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || typingIn(event.target))
      return;
    if (naming) return;
    if (event.code === 'KeyF') {
      if (isFull) exit();
      else enter();
    } else if (event.code === 'Escape' && isFull && stage.picking !== true) exit();
    else if (event.code === 'KeyC' && commenting) stage.onPick();
  });
  useEffect(() => {
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);
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
        onOpenOutside={openOutside}
        onCopyLink={() => void navigator.clipboard?.writeText(location.href).catch(none)}
        onRetry={() => setReloadKey((n) => n + 1)}
        onOpenDrawer={none}
        onToggleMax={none}
        maxed={false}
        stage={commenting ? stage : undefined}
        full={isFull}
        memoryKey={`guest:${config.token}`}
        viewSeed={view}
        guest={{
          pickLabel: t('next.guest.pick'),
          pickTip: t('next.guest.pickTip'),
          account:
            config.rights === 'comment' && who.name !== ''
              ? { label: t('next.guest.account', { name: who.name }), onOpen: () => setNaming(true) }
              : null,
          full: { label: t('next.guest.full'), tip: t('next.guest.fullTip'), onOpen: enter },
        }}
      />
      {!isFull && (
        <div className="hx-guest__dock">
          {touch && (
            <button type="button" className="hx-guest__go" onClick={enter}>
              <Icon name="maximize-2" size="sm" />
              {t('next.guest.fullPhone')}
            </button>
          )}
          {commenting && <NoteStrip notes={notes} readOnly onEdit={none} onRemove={none} onClear={none} />}
        </div>
      )}
      {isFull && (
        <GuestFull
          touch={touch}
          onExit={exit}
          comment={commenting ? { picking: stage.picking === true, onToggle: stage.onPick } : null}
          onNewTab={openOutside}
        />
      )}
      {commenting && (
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
            if (who.name === '') afterName();
          }}
        />
      )}
    </>
  );
};

const GuestApp = ({ config }: { config: IGuestConfig | null }) => {
  const portal = useRef<HTMLDivElement>(null);
  const [full, setFull] = useState<TFull>(FULL.off);
  useEffect(() => {
    if (config) document.title = config.title;
  }, [config]);
  return (
    <PortalContext value={portal}>
      <div className="hx-root hx-guest" data-full={full}>
        {config ? (
          <main className="hx-main">
            {config.kind === 'slides' ? (
              <GuestDeck config={config} />
            ) : (
              <GuestBody config={config} full={full} onFull={setFull} />
            )}
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
