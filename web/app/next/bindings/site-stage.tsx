// The chat of an open design over the machine. The design's chat is bound to it (bindings/design-conv.ts): it opens at
// once from what the browser kept, and a message goes to that chat, never to another one of the folder. Open beside the
// site (a window or an edge) it is the usual chat — the same feed, field, picker, queue and states as anywhere; folded
// over the site it is the island: the field, and over it the agent's last line with what it does now and «Стоп». A
// design without a chat gets one with its first message, which is in the column from the moment it is sent.
import { useTourFill } from '@/next/features/tour/use-tour-fill';
import { useTourStep } from '@/next/features/tour/use-tour-step';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { interrupt, sendMessage } from '@/next/commands';
import { uploadSrc } from '@/next/commands/uploads';
import ChatView, { type TChatOpeners } from '@/next/bindings/chat-view';
import { openFileInChat } from '@/next/bindings/open-file-in-chat';
import { keepLayout, keptLayout } from '@/next/features/designly/site-layout';
import { openPlainOnce } from '@/next/features/designly/chat-place';
import { noteLayoutKey } from '@/next/stores/chat-view';
import ChatDock, { type IDockExtra } from '@/next/bindings/chat-dock';
import { outgoingItem, useFeedItems } from '@/next/bindings/feed-items';
import { useAgentLimits } from '@/next/bindings/use-limits';
import { limitsOwnerOf } from '@/next/features/limits/model';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { useUploads } from '@/next/bindings/use-uploads';
import { useVoice } from '@/next/bindings/use-voice';
import {
  designBound,
  settleDesignCreating,
  startDesignConv,
  useDesignConv,
  useDesignCreating,
  type IDesignCreating,
} from '@/next/bindings/design-conv';
import { useDesignChatsFound } from '@/next/bindings/design-chats';
import { hub, useConversationChannel } from '@/next/core/realtime/hub';
import Feed from '@/next/features/chat/feed';
import type { FeedItem } from '@/next/features/chat/types';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import ScreenField from '@/next/features/composer/one-field';
import { ChatSwitch } from '@/next/features/designly/chat-switch';
import { DESIGN_CONV, designChats, designKey, type TDesignConv } from '@/next/features/designly/design-conv';
import { chatStatusOf } from '@/next/features/sidebar/status';
import { lastLineOf } from '@/next/features/designly/last-line';
import {
  firstFloat,
  IslandLast,
  ISLAND_STATUS,
  SiteChat,
  SiteIsland,
  type ISiteChatProps,
  type IChatMoreItem,
  type TIslandStatus,
  type TSiteMode,
} from '@/next/features/designly/site-chat';
import { ChatOpener } from '@/next/bindings/chat-handoff';
import { requestContinue } from '@/next/stores/ui-requests';
import { requestMoveVm, useMoveEnabled } from '@/next/stores/chat-move';
import type { IFloat, TDock, TZone } from '@/next/features/designly/site-drag';
import type { ISiteStage } from '@/next/features/designly/site-view';
import { formatAnnotation, withAnnotations } from '@/next/features/designly/annotation';
import {
  DECK_CTX_ID,
  deckContextQuote,
  formatChatContext,
  type IDeckChatContext,
} from '@/next/features/designly/deck/chat-context';
import { anchorItems } from '@/next/features/designly/note-anchor';
import { itemsOf, NO_EDIT } from '@/next/features/designly/element-style';
import {
  NotePins,
  NotePopover,
  noteChip,
  NotesBar,
  NOTE_OUT_MS,
  type INote,
  type INotePopoverProps,
} from '@/next/features/designly/site-notes';
import { usePageDraft } from '@/next/features/designly/page-drafts';
import { boardFileOf, useBoardVeils } from '@/next/features/designly/image-gen/canvas';
import type { Conversation, ConvId, MachineId, Now } from '@/next/protocol/v2';
import { goTo, ROUTE, type TRoute } from '@/next/routes/routes';
import { useConnection } from '@/next/stores/connection';
import { useFeed, useNow } from '@/next/stores/conversation';
import { useConversation, useConversationList } from '@/next/stores/conversations';
import { clearDraft, setDraft, useDraft, type DraftKey } from '@/next/stores/drafts';
import { limitsOf, useMachine, useMachines } from '@/next/stores/machines';
import { useEnterSends, useMe } from '@/next/stores/me';
import { useMessage } from '@/next/stores/messages';
import { AttachmentSrc } from '@/next/ui/attachment-thumb';
import { bindLaunch } from '@/next/ui/flight';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { toast } from '@/next/ui/toast';

/**
 * The agent's state as the folded chat shows it; `done` is the end of a turn the person saw running. A first message
 * on its way (its chat being made) is the agent starting, as in the usual chat.
 */
const statusOf = (
  conv: Conversation | null,
  now: Now | null,
  sawRun: boolean,
  launching: boolean,
): TIslandStatus => {
  if (!conv) return launching ? { k: ISLAND_STATUS.run, text: t('next.site.starting') } : null;
  const st = conv.state;
  if (st.k === 'starting') return { k: ISLAND_STATUS.run, text: t('next.site.starting') };
  if (st.k === 'working')
    return { k: ISLAND_STATUS.run, text: now?.action?.summary ?? t('next.now.working') };
  if (st.k === 'waiting-question' || st.k === 'waiting-permission')
    return { k: ISLAND_STATUS.wait, text: t('next.site.waits') };
  if (st.k === 'launch-failed' || st.k === 'gone')
    return { k: ISLAND_STATUS.wait, text: t('next.site.agentFailed') };
  if (sawRun && st.k === 'ready') return { k: ISLAND_STATUS.done, text: t('next.site.done') };
  return null;
};

/** The site's `data-run`: a finished turn's status rides above the edits bar. */
const runOf = (status: TIslandStatus): ISiteStage['run'] => {
  if (status?.k === ISLAND_STATUS.run) return 'run';
  if (status?.k === ISLAND_STATUS.done) return 'done';
  return 'idle';
};

/** The last thing the agent said in a chat's feed, as one line. */
const lastAnswer = (items: readonly FeedItem[] | null): string | null => {
  for (let i = (items?.length ?? 0) - 1; i >= 0; i -= 1) {
    const item = items?.[i];
    if (item?.kind === 'turn' && item.answer) return lastLineOf(item.answer.md);
  }
  return null;
};

/** The folded chat's last line of a design that has a chat. */
const ConvLast = ({
  conv,
  machine,
  onExpand,
}: {
  conv: ConvId;
  machine: MachineId;
  onExpand: () => void;
}) => {
  const items = useFeedItems(conv, machine);
  return <IslandLast text={lastAnswer(items)} onExpand={onExpand} />;
};

/** The card of a note with its own microphone: what is dictated lands in the card's field; `onTextChange` keeps the
 *  words with the page's card, so they are there again when its page comes back. */
export const NoteCard = ({
  machine,
  onTextChange,
  design,
  pageUrl,
  ...props
}: {
  machine: MachineId;
  onTextChange?: (text: string) => void;
  /** The design and the page on screen: a picture can be drawn into a block of a board of it. */
  design?: string;
  pageUrl?: string;
} & Omit<INotePopoverProps, 'text' | 'onText' | 'mic' | 'image'>) => {
  const file = design !== undefined && pageUrl !== undefined ? boardFileOf(design, pageUrl) : null;
  const [text, setTextHere] = useState(props.draft.text);
  // Words that come after the card was attached, cancelled or deleted (the voice written down late) are not the
  // card's any more: they would open it again and let it be attached twice.
  const settled = useRef(false);
  const setText = (value: string) => {
    if (settled.current || props.hidden) return;
    setTextHere(value);
    onTextChange?.(value);
  };
  const settle =
    <A extends unknown[]>(then: (...args: A) => void) =>
    (...args: A) => {
      settled.current = true;
      then(...args);
    };
  const voice = useVoice(machine, text, setText);
  useTourFill(text, setText, 'note');
  const start = voice.onMic;
  const mic = start && {
    recording: voice.recording !== null,
    onToggle: () => {
      if (!voice.recording) start();
      else if (!voice.recording.busy) voice.onFinishRecording();
    },
  };
  return (
    <NotePopover
      {...props}
      onOk={settle(props.onOk)}
      onCancel={settle(props.onCancel)}
      onDelete={settle(props.onDelete)}
      text={text}
      onText={setText}
      mic={mic ?? null}
      image={design !== undefined && file !== null ? { machine, artifact: design, file } : undefined}
    />
  );
};

const none = (): void => {};
const done = (): Promise<void> => Promise.resolve();

interface IStart {
  choice: IModelChoice | null;
  agents: IPickerAgent[];
  onPick: (choice: IModelChoice) => void;
}

interface INewChatProps {
  machine: MachineId;
  draftKey: DraftKey;
  creating: IDesignCreating | undefined;
  start: IStart;
  extra: IDockExtra;
  /** Sends the first message; the client id of its bubble, or null when it did not go. */
  onFirst: (text: string, attachments: ReturnType<typeof useUploads>['ready']) => string | null;
}

/** The field of a design without a chat: the usual one with the model to start on; the first message makes the chat. */
const NewDesignField = ({ machine, draftKey, creating, start, extra, onFirst }: INewChatProps) => {
  const enterSends = useEnterSends();
  const draft = useDraft(draftKey);
  const text = draft?.text ?? '';
  const files = useMemo(() => draft?.attachments ?? [], [draft]);
  const uploads = useUploads(machine, draftKey, files);
  const onChange = useCallback(
    (value: string) => setDraft(draftKey, { text: value, attachments: files }),
    [draftKey, files],
  );
  const voice = useVoice(machine, text, onChange);
  useTourFill(text, onChange);
  const limits = useAgentLimits(machine, limitsOwnerOf(start.choice));
  const srcOf = useCallback((item: { id: string }) => uploadSrc(machine, item), [machine]);

  const onSubmit = () => {
    if (creating) {
      toast(t('next.site.creatingWait'), { tone: 'info' });
      return;
    }
    if (uploads.uploading) {
      toast(t('next.composer.uploadingWait'), { tone: 'info' });
      return;
    }
    const clientId = onFirst(extra.wrap(text.trim()), uploads.ready);
    if (!clientId) return;
    bindLaunch(clientId);
    clearDraft(draftKey);
    uploads.reset();
    extra.onSent();
  };

  return (
    <AttachmentSrc.Provider value={srcOf}>
      <ScreenField
        glideIn="site"
        enterSends={enterSends}
        value={text}
        onChange={onChange}
        onSubmit={onSubmit}
        mode="new"
        onStop={done}
        picker={
          start.choice && {
            value: start.choice,
            agents: start.agents,
            locked: false,
            onChange: start.onPick,
          }
        }
        quotes={extra.quotes}
        onRemoveQuote={extra.onRemoveQuote}
        noteGroups={extra.notes ? [extra.notes] : undefined}
        tools={extra.tools}
        placeholder={extra.placeholder}
        attachments={uploads.attachments}
        onFiles={uploads.onFiles}
        onRemoveAttachment={uploads.onRemove}
        onRetryAttachment={uploads.onRetry}
        {...voice}
        limits={limits}
        maxText={limitsOf(machine).maxText}
      />
    </AttachmentSrc.Provider>
  );
};

/**
 * A design without a chat: the usual field with the model to start on, over a feed that is empty until the first
 * message, which stands there from the moment it is sent (pending → sent) while its chat is being made.
 */
const NewDesignChat = (props: INewChatProps) => {
  const { machine, creating } = props;
  const me = useMe();
  const connection = useConnection(machine);
  const machineView = useMachine(machine);
  const first = useMessage(creating?.clientId ?? null);
  const items = useMemo(() => (first ? [outgoingItem(first)] : []), [first]);
  const [openedAt] = useState(() => Date.now());
  const srcOf = useCallback((item: { id: string }) => uploadSrc(machine, item), [machine]);

  return (
    <AttachmentSrc.Provider value={srcOf}>
      <Feed
        items={items}
        hasOlder={false}
        loadingOlder={false}
        onLoadOlder={none}
        density={me.k === 'ready' ? me.me.settings.density : 'compact'}
        offline={connection !== undefined && connection.k !== 'live'}
        enterAfter={openedAt}
        machineName={machineView?.machine.name ?? machine}
        onContinueTurn={done}
        onRetryTurn={done}
        onRetryMessage={done}
        onDeleteMessage={done}
        dock={<NewDesignField {...props} />}
      />
    </AttachmentSrc.Provider>
  );
};

/**
 * The folded chat's field over the site: the design's chat's own dock while it has one — the queue, «Now»,
 * stop, the model —, before that the new design chat's field. The same one the open chat shows.
 */
const IslandField = ({
  conv,
  designDraft,
  ...rest
}: Omit<INewChatProps, 'draftKey'> & { conv: ConvId | null; designDraft: DraftKey }) =>
  conv ? (
    <div className="hx-dock" data-screen>
      <ChatDock conv={conv} machine={rest.machine} extra={rest.extra} />
    </div>
  ) : (
    <div className="hx-dock" data-screen>
      <NewDesignField draftKey={designDraft} {...rest} />
    </div>
  );

interface IDesignChatProps extends Omit<INewChatProps, 'draftKey'> {
  picked: TDesignConv;
  designDraft: DraftKey;
  onOpenChat: () => void;
  /** A chat this one gave way to (another model, a fork) comes into the design's column. */
  onPickChat: (id: string) => void;
  open?: TChatOpeners;
}

/** The open chat's body: the design's current chat as it is anywhere, or a new one for the design. */
const DesignChat = ({ picked, designDraft, onOpenChat, onPickChat, open, ...rest }: IDesignChatProps) => {
  const { machine, creating, extra } = rest;
  const first = useMessage(creating?.clientId ?? null);
  const id = picked.k === DESIGN_CONV.conv ? (picked.id as ConvId) : null;
  // The chat the first message made, until its own feed is read: its message stands where it will stay.
  const pendingItems = useMemo(
    () => (first && creating?.conv === id ? [outgoingItem(first)] : null),
    [first, creating?.conv, id],
  );
  if (id)
    return (
      <ChatOpener.Provider value={onPickChat}>
        <ChatView
          key={id}
          conv={id}
          machine={machine}
          onRetryLoad={() => hub.client(machine)?.reconnectNow()}
          onShowBackground={onOpenChat}
          onOpenFile={(path) => openFileInChat(id, machine, path, onOpenChat)}
          dockExtra={extra}
          pendingItems={pendingItems}
          {...open}
        />
      </ChatOpener.Provider>
    );
  return <NewDesignChat draftKey={designDraft} {...rest} />;
};

/** The leaving chat's fade before the next one, as .hx-site__conv[data-swap] in site.css. */
const SWAP_MS = 160;

/**
 * The chat on screen across a pick: the one that was there for its fade, then the picked one. Only a pick (`epoch`)
 * fades: a chat that the first message made comes in where the new one stood.
 */
const useSwap = (picked: TDesignConv, epoch: number) => {
  const [view, setView] = useState({ epoch, picked });
  const leaving = view.epoch !== epoch;
  const latest = useRef(picked);
  useEffect(() => {
    latest.current = picked;
  });
  useEffect(() => {
    if (!leaving) return;
    const timer = setTimeout(() => setView({ epoch, picked: latest.current }), SWAP_MS);
    return () => clearTimeout(timer);
  }, [leaving, epoch]);
  return { shown: leaving ? view.picked : picked, leaving };
};

let noteSeq = 0;

const CHAT_WIDTH = 372;

export interface IStageOptions {
  machine: MachineId;
  /** The design's id in the library. */
  design: string;
  /** The chat the person came from (`?chat=`): the column shows it until they pick another. */
  fromChat: string | null;
  /** The project's folder; null while the library loads. */
  dir: string | null;
  /** The design's chats this machine has: started from it, then the ones that edited its files (`convs`, `touchedBy`). */
  tied: readonly string[];
  title: string;
  /** The page on screen, its path on the library server: its notes are on it. */
  pageUrl: string;
  /** The library server's port; null while the library loads. */
  port: number | null;
  onOpenDrawer: () => void;
  /** Where a link, a file or a card of the design's chat opens (the pane beside the design); without it they stay put. */
  open?: TChatOpeners;
  /** The design's own page keeps where its chat stands; the right pane's copy has its own choice (pane-chat.ts). */
  keep?: boolean;
  /**
   * An app's object or home in Sinopia's stage instead of a design of the library: its chats are not recorded in the
   * library nor asked of other machines, a pick of a chat leaves `?chat=` for `address`, and a chat made here is handed
   * to `onCreated` (the app turns itself on in it).
   */
  app?: { address: TRoute; onCreated: (conv: ConvId) => void };
  /**
   * A space of the person's own (boards, documents; HRN-1391): the space takes the whole screen with the one field over
   * it; the first words or `@harnessly` open the chat in a window over the space, rising from the field. No chat comes
   * back by itself: a chat bound here before this page was opened is let go. Where the person puts the chat is kept
   * for the whole app (`layoutKey`), not for each board.
   */
  space?: { layoutKey: string };
}

/** `@harnessly` in the field: the person calls the agent over the space. */
export const MENTION_RE = /(^|\s)@harnessly\b/i;
/** The mention at the start of the words is the call, not the request: the agent reads the rest. */
export const withoutMention = (words: string): string => words.replace(/^\s*@harnessly\b[\s,:]*/i, '');

interface IBox {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * Where the window is placed: the section's box inside its padding, which is what the window's left and top count from
 * (it measured from the outer box went past the section's edge by the padding).
 */
const placeBox = (el: HTMLElement | null) => {
  if (!el) return null;
  const box = el.getBoundingClientRect();
  const cs = getComputedStyle(el);
  const pad = (side: string) => parseFloat(cs.getPropertyValue(`padding-${side}`)) || 0;
  const left = box.left + el.clientLeft + pad('left');
  const top = box.top + el.clientTop + pad('top');
  return {
    left,
    top,
    width: el.clientWidth - pad('left') - pad('right'),
    height: el.clientHeight - pad('top') - pad('bottom'),
  };
};

/** A space's chat window before its first message: the head and the field with a context chip. */
const QUIET_FLOAT_H = 212;

/** The narrowest chat window beside a selection; less than this and it goes over the selection's edge. */
const NARROW_FLOAT = 320;

/**
 * The window of a space's chat beside a selection (in the section's pixels): to its right where it fits, else to its
 * left, else under or over it; always inside the section.
 */
export const nearFloat = (sel: IBox, section: { width: number; height: number }): IFloat => {
  const { w, h } = fieldFloat(section);
  const gap = 16;
  const clampX = (x: number) => Math.min(Math.max(gap, x), Math.max(gap, section.width - w - gap));
  const clampY = (y: number) => Math.min(Math.max(gap, y), Math.max(gap, section.height - h - gap));
  if (sel.x + sel.w + gap + w <= section.width - gap)
    return { x: sel.x + sel.w + gap, y: clampY(sel.y), w, h };
  if (sel.x - gap - w >= gap) return { x: sel.x - gap - w, y: clampY(sel.y), w, h };
  if (sel.y + sel.h + gap + h <= section.height - gap)
    return { x: clampX(sel.x), y: sel.y + sel.h + gap, w, h };
  if (sel.y - gap - h >= gap) return { x: clampX(sel.x), y: sel.y - gap - h, w, h };
  // Nothing whole fits: a narrower window on the roomier side, never under the selection while it has room for one.
  const right = section.width - gap - (sel.x + sel.w + gap);
  const left = sel.x - gap - gap;
  const room = Math.max(right, left);
  if (room >= NARROW_FLOAT) {
    const narrow = Math.min(w, room);
    return { x: right >= left ? sel.x + sel.w + gap : sel.x - gap - narrow, y: clampY(sel.y), w: narrow, h };
  }
  return { x: clampX(sel.x), y: clampY(sel.y - gap - h), w, h };
};

/** The window of a space's chat: over the space, rising from the field at the bottom, at the middle. */
export const fieldFloat = (section: { width: number; height: number }): IFloat => {
  const w = Math.min(420, Math.max(280, section.width - 32));
  const h = Math.max(280, Math.min(520, section.height - 200));
  return {
    x: Math.max(16, Math.round((section.width - w) / 2)),
    y: Math.max(16, section.height - h - 132),
    w,
    h,
  };
};

/**
 * The chat each space was bound to when this page first opened it: it is not shown again. A chat made over
 * the space in this page is another one, and stays with it.
 */
const letGo = new Map<string, string | null>();

/** Everything the site view needs to show the chat beside it and the field over it. */
export const useSiteStage = ({
  machine,
  design,
  fromChat,
  dir,
  tied,
  title,
  pageUrl,
  port,
  onOpenDrawer,
  open,
  keep = false,
  app,
  space,
}: IStageOptions): ISiteStage => {
  const key = designKey(machine, design);
  // A space opens without the chat bound to it before: that one was the chat the person happened to be in.
  if (space && !letGo.has(key)) letGo.set(key, fromChat === null ? designBound(key) : null);
  const layoutKey = space ? designKey(machine, space.layoutKey) : key;
  const current = useDesignConv(machine, design, fromChat, app?.address, space ? letGo.get(key) : null);
  const { picked } = current;
  const machines = useMachines();
  const machineNames = useMemo(
    () => new Map((machines ?? []).map((v) => [v.machine.id as string, v.machine.name])),
    [machines],
  );
  const connection = useConnection(machine);
  const moveOn = useMoveEnabled(machine);
  // The chats of every machine: the switch lists the design's chats wherever they are.
  const everywhere = useConversationList(null);
  const [asked, setAsked] = useState(0);
  const found = useDesignChatsFound(machine, design, tied, asked, app !== undefined);
  // A pick in the switch: the chat on screen gives way, the next one comes in.
  const [epoch, setEpoch] = useState(0);
  const swap = useSwap(picked, epoch);
  const creating = useDesignCreating(key);
  const convId = picked.k === DESIGN_CONV.conv ? (picked.id as ConvId) : null;
  const conv = useConversation(convId) ?? null;
  useConversationChannel(convId ? machine : null, convId);
  const now = useNow(convId);
  const feed = useFeed(convId);
  const start = useStartChoice(machine);

  // The first message's bubble is the column's until the new chat's own feed has it.
  useEffect(() => {
    if (creating?.conv && creating.conv === convId && feed?.loaded) settleDesignCreating(key);
  }, [creating?.conv, convId, feed?.loaded, key]);

  // Where the chat stands comes back as the person left it on this design (site-layout.ts).
  // Unless the person placed it by hand, the chat of a design being edited stands docked on the left, not over the
  // page: a place kept only because it was the old default does not count.
  const [kept] = useState(() => {
    const layout = keep ? keptLayout(layoutKey) : null;
    return layout?.chosen ? layout : null;
  });
  // A space: the one field over it until the person calls the agent.
  const [mode, setMode] = useState<TSiteMode>(kept?.mode ?? (space ? 'island' : 'chat'));
  const [min, setMin] = useState(kept?.min ?? false);
  const [chosen, setChosen] = useState(kept !== null);
  const [float, setFloat] = useState<IFloat | null>(kept?.float ?? null);
  const [dock, setDock] = useState<TDock>(kept?.dock ?? 'left');
  const [chatW, setChatW] = useState(kept?.chatW ?? CHAT_WIDTH);
  const [sview, setSview] = useState(false);
  // Where the chat stood open last: the folded one opens there again.
  const [opened, setOpened] = useState<TSiteMode>(kept?.opened ?? (space ? 'float' : 'chat'));
  // The chat of this screen keeps its place with the chat's view on the machine (stores/chat-view.ts).
  useEffect(() => {
    if (keep && convId) noteLayoutKey(convId, key);
  }, [keep, convId, key]);
  useEffect(() => {
    if (keep) keepLayout(layoutKey, { mode, min, dock, chatW, float, opened, chosen });
  }, [keep, layoutKey, mode, min, dock, chatW, float, opened, chosen]);

  // One draft for the folded field and the open one: the chat's, or the design's own before it has a chat.
  const designDraft: DraftKey = `design:${key}`;
  const draftKey: DraftKey = convId ?? designDraft;
  const draft = useDraft(draftKey);
  const text = draft?.text ?? '';
  const setText = (value: string) =>
    setDraft(draftKey, { text: value, attachments: draft?.attachments ?? [] });
  useTourFill(text, setText);

  const [notes, setNotes] = useState<INote[]>([]);
  // The blocks of the page on screen a picture is being drawn into.
  const veils = useBoardVeils(design, pageUrl);
  // A deck's slide (and block) the chat is about: one chip that goes with the next message, then lets go.
  const [deckCtx, setDeckCtx] = useState<IDeckChatContext | null>(null);
  // The card is the page's: another page hides it, the return brings it back.
  const [noteDraft, setNoteDraft] = usePageDraft(pageUrl);
  const card = usePresence(noteDraft, NOTE_OUT_MS);
  const shown = card.value;
  const [picking, setPicking] = useState(false);
  const [bridged, setBridged] = useState(false);
  // The blocks the page said it no longer has: a look cannot be shown on them.
  const [goneBlocks, setGoneBlocks] = useState<readonly number[]>([]);

  // «Готово» is for a turn the person saw running here, until it is dismissed or the next one starts.
  const [sawRun, setSawRun] = useState(false);
  const running = conv?.state.k === 'working' || conv?.state.k === 'starting';
  if (running && !sawRun) setSawRun(true);

  // «Открыть чат»: the plain chat page, once; opened again from anywhere the chat is the design with it (chat-place.ts).
  const openChatPage = () => {
    if (!convId) return;
    openPlainOnce(convId);
    goTo({ kind: ROUTE.chat, id: convId });
  };
  const status = statusOf(conv, now, sawRun, creating !== undefined);

  /** The typed words with the notes as the agent reads them. */
  const wrap = (typed: string) =>
    withAnnotations(space ? withoutMention(typed) : typed, [
      ...notes.map((note, i) =>
        formatAnnotation({
          n: i + 1,
          picked: note.picked,
          comment: note.text,
          style: note.style,
          url: note.page,
          port: port ?? 0,
        }),
      ),
      ...(deckCtx ? [formatChatContext(notes.length + 1, deckCtx)] : []),
    ]);
  const afterSend = () => {
    setSawRun(false);
    setNotes([]);
    setPicking(false);
  };
  /** The first message of a design without a chat: it makes the one chat, bound to the design. */
  const sendFirst = (words: string, attachments: ReturnType<typeof useUploads>['ready']) => {
    if (dir === null) {
      toast(t('next.newChat.connecting'), { tone: 'info' });
      return null;
    }
    return startDesignConv({
      machine,
      design,
      key,
      dir,
      choice: start.choice,
      text: words,
      attachments,
      onRefused: (back) => setDraft(designDraft, { text: back, attachments: [] }),
      ...(app ? { unlisted: true, onCreated: app.onCreated } : {}),
    });
  };

  // A space's window with no message yet is only its head and field, over the space rather than across it; the first
  // message grows it to its size.
  const quiet = space !== undefined && convId === null && !creating;
  const shownFloat = quiet && float ? { ...float, h: Math.min(float.h, QUIET_FLOAT_H) } : float;
  const empty = !text.trim() && notes.length === 0;
  const canSend = !empty && !(creating && !convId);
  // The folded field: to the chat's queue as the usual field does, or the design's first message.
  const send = () => {
    if (!canSend) return;
    const words = wrap(text.trim());
    if (convId) {
      sendMessage(machine, { conversationId: convId, text: words, mode: 'queue' });
      clearDraft(convId);
      afterSend();
      return;
    }
    if (!sendFirst(words, [])) return;
    clearDraft(designDraft);
    afterSend();
  };

  const removeNote = (id: string) => setNotes((list) => list.filter((n) => n.id !== id));
  // The pins and the looks are of the page on screen; the strip over the field lists the notes of every page.
  const pageNotes = notes.filter((note) => note.page === pageUrl);
  // A note opens in its card again from its pin or from the list.
  const editNote = (id: string) => {
    const note = notes.find((n) => n.id === id);
    if (!note) return;
    setPicking(false);
    setNoteDraft({ picked: note.picked, at: note.at, id, text: note.text, style: note.style });
  };
  const editing = noteDraft?.id === undefined ? -1 : notes.findIndex((n) => n.id === noteDraft.id);
  const noteNumber = editing >= 0 ? editing + 1 : notes.length + 1;

  const onPick = () => {
    setMin(false);
    setPicking((on) => !on);
  };
  const leaveTo = (next: TSiteMode) => {
    setMode(next);
    if (next === 'chat' || next === 'float') setOpened(next);
    setSview(false);
    setPicking(false);
    setNoteDraft(null);
  };
  // The section the stage stands in, and a space's selection on the page: the window opens beside it.
  const sectionEl = useRef<HTMLElement | null>(null);
  const selection = useRef<IBox | null>(null);
  // A space's chat comes over it where the person last put it; the first time, a window rising from the field.
  const callOver = () => {
    if (mode !== 'island') return;
    // A window the person placed stays where they put it; otherwise beside the selection, or over the field.
    if (opened === 'float' && !(chosen && float)) {
      const box = placeBox(sectionEl.current);
      const size = { width: box?.width ?? window.innerWidth, height: box?.height ?? window.innerHeight };
      const sel = selection.current;
      setFloat(
        sel && box
          ? nearFloat({ x: sel.x - box.left, y: sel.y - box.top, w: sel.w, h: sel.h }, size)
          : fieldFloat(size),
      );
    }
    leaveTo(opened === 'float' ? 'float' : 'chat');
    setMin(false);
  };
  // `@harnessly` typed in the folded field calls the chat over the space, with the words in its field.
  const called = space !== undefined && mode === 'island' && MENTION_RE.test(text);
  const callRef = useRef(callOver);
  useEffect(() => {
    callRef.current = callOver;
  });
  useEffect(() => {
    if (called) callRef.current();
  }, [called]);
  // A lesson that edits the design keeps its chat docked beside the page: the field over the page hides what changes.
  const sideChat = useTourStep()?.sideChat === true;
  // Only when the step comes (adjusted while rendering): the person may move the chat back while it is on.
  const [sideAsked, setSideAsked] = useState(false);
  if (sideChat !== sideAsked) {
    setSideAsked(sideChat);
    if (sideChat && mode !== 'chat') leaveTo('chat');
  }
  // Where the chat was dragged: it settles there with the field open; a pull of the field down folds it.
  const onZone = (zone: TZone, drop: IFloat | null) => {
    if (zone === 'min') {
      setMin(true);
      return;
    }
    setChosen(true);
    if (zone === 'left' || zone === 'right') setDock(zone);
    // A quiet window moved keeps the size it grows to with the first message.
    if (zone === 'float' && drop) setFloat(quiet && float ? { ...drop, h: float.h } : drop);
    leaveTo(zone === 'left' || zone === 'right' ? 'chat' : zone);
    setMin(false);
  };
  // The folded chat opens beside the site, where it was last (an edge by default).
  const expand = () => {
    if (space) {
      callOver();
      return;
    }
    leaveTo(opened === 'float' && float ? 'float' : 'chat');
    setMin(false);
  };

  // The notes ride in the open field as chips, with the page's «pick a block» on its bar.
  const pickTool = bridged ? (
    <button
      type="button"
      className="hx-head-btn hx-tipped"
      aria-pressed={picking}
      aria-label={t('next.notes.pick')}
      onClick={onPick}
    >
      <Icon name="square-dashed-mouse-pointer" />
      <span className="hx-tip" data-align="end">
        {t('next.notes.pickTip')}
        <kbd>V</kbd>
      </span>
    </button>
  ) : null;
  const extra: IDockExtra = {
    quotes: deckCtx ? [deckContextQuote(deckCtx)] : [],
    notes: {
      key: 'design',
      notes: notes.map(noteChip),
      onEdit: (id, words) => setNotes((list) => list.map((n) => (n.id === id ? { ...n, text: words } : n))),
      onRemove: removeNote,
      onOpen: (id) => {
        setMin(false);
        editNote(id);
      },
    },
    placeholder: deckCtx
      ? t(deckCtx.selected ? 'next.deck.chat.placeholderSelected' : 'next.deck.chat.placeholder')
      : undefined,
    onRemoveQuote: (id) => (id === DECK_CTX_ID ? setDeckCtx(null) : removeNote(id)),
    wrap,
    onSent: () => {
      afterSend();
      // The first words over a space open its chat: the answer is read where it was asked.
      if (space) callOver();
    },
    tools: pickTool,
  };

  const rowsOf = designChats({ list: everywhere, found, fresh: creating?.conv ?? null });
  const pickChat = (id: string | null) => {
    const row = id === null ? undefined : rowsOf.find((c) => c.id === id);
    // A chat of another machine opens on its machine, in its own page: the design's column is this machine's.
    if (row && row.machineId !== machine) {
      goTo({ kind: ROUTE.chat, id: row.id });
      return;
    }
    current.pick(id);
    setEpoch((n) => n + 1);
  };
  const online = connection === undefined || connection.k === 'live';
  const rows = rowsOf.map((c) => ({
    id: c.id,
    title: c.title,
    machine: c.machineId === machine ? null : (machineNames.get(c.machineId) ?? c.machineId),
    status: chatStatusOf(c, online),
    at: c.lastActivityAt,
  }));
  const changeMode = (next: TSiteMode, section: { width: number; height: number }) => {
    if (next === mode) return;
    setChosen(true);
    if (next === 'float' && !float) setFloat(firstFloat(section));
    leaveTo(next);
    setMin(next === 'island' && empty);
  };
  // The usual chat's «Продолжить в другой модели» and «… на другой ВМ», for the design's chat too.
  const more: IChatMoreItem[] = convId
    ? [
        {
          id: 'model',
          icon: 'arrow-left-right',
          label: t('next.site.continueModel'),
          pick: () => requestContinue(convId),
        },
        ...(moveOn
          ? [
              {
                id: 'vm',
                icon: 'server',
                label: t('next.site.continueMachine'),
                pick: () => requestMoveVm(convId),
              },
            ]
          : []),
      ]
    : [];
  // The switch names the chat; before it has one, what the new chat is about: the design, or the space.
  const chatTitle = () => {
    if (convId) return title;
    if (space) return t('next.site.newSpaceChat', { name: title });
    return t('next.site.newDesignChat');
  };
  const chat: ISiteChatProps = {
    title,
    heading: (
      <ChatSwitch
        current={convId}
        title={conv?.title ?? chatTitle()}
        rows={rows}
        onPick={pickChat}
        onOpen={() => setAsked((n) => n + 1)}
      />
    ),
    swapping: swap.leaving,
    mode,
    float: shownFloat,
    onFloat: setFloat,
    dock,
    width: chatW,
    onWidth: setChatW,
    onZone,
    onHide: () => {
      leaveTo('island');
      setMin(empty);
    },
    onOpenDrawer,
    onMode: changeMode,
    more,
    onOpenSite: () => setSview(true),
  };

  return {
    conv: convId,
    mode,
    onMode: changeMode,
    min: mode === 'island' && min,
    run: runOf(status),
    busy: status?.k === ISLAND_STATUS.run,
    // «Правки» pick all the time, but not while a note is being written.
    pick: bridged && noteDraft === null && (mode === 'notes' || picking),
    // The browser bar's «Выделить» and the field's button are one switch; null until the page has its bridge.
    picking: bridged ? picking || mode === 'notes' : null,
    onPick,
    onPicked: (picked, at) => {
      setPicking(false);
      setNoteDraft({ picked, at, text: '', style: NO_EDIT });
    },
    onPickEnd: () => setPicking(false),
    onBridge: setBridged,
    // What the page wears: the look of every note, the card's own edit in place of its note's.
    styles: itemsOf(pageNotes, noteDraft),
    anchors: anchorItems(pageNotes, noteDraft),
    onStyled: setGoneBlocks,
    sview,
    onSview: setSview,
    // A line of the page's tools lands in the field, after what is typed there; the field opens to show it.
    onAttach: (words) => {
      setText(text.trim() === '' ? words : `${text.trimEnd()}\n${words}`);
      setMin(false);
    },
    veils,
    pins: <NotePins notes={pageNotes} draft={noteDraft} onOpen={editNote} />,
    overlay: (
      <>
        {shown && (
          <NoteCard
            key={`${shown.id ?? shown.picked.selector}@${shown.at.x},${shown.at.y}`}
            machine={machine}
            design={design}
            pageUrl={pageUrl}
            n={noteNumber}
            draft={shown}
            hidden={card.state === PRESENCE.closing}
            onTextChange={(text) => setNoteDraft({ ...shown, text })}
            needsText={mode === 'notes'}
            gone={shown.picked.id !== null && goneBlocks.includes(shown.picked.id)}
            onStyle={(style) => setNoteDraft({ ...shown, style })}
            onOk={(words) => {
              if (shown.id) {
                setNotes((list) =>
                  list.map((n) => (n.id === shown.id ? { ...n, text: words, style: shown.style } : n)),
                );
              } else {
                setNotes((list) => [
                  ...list,
                  {
                    id: `n${++noteSeq}`,
                    picked: shown.picked,
                    page: pageUrl,
                    text: words,
                    at: shown.at,
                    style: shown.style,
                  },
                ]);
              }
              setNoteDraft(null);
              setMin(false);
            }}
            onCancel={() => setNoteDraft(null)}
            onDelete={() => {
              if (shown.id) removeNote(shown.id);
              setNoteDraft(null);
            }}
          />
        )}
        <NotesBar notes={notes} shown={mode === 'notes' && status?.k !== ISLAND_STATUS.run} onSend={send} />
      </>
    ),
    float: shownFloat,
    dock,
    chatW,
    context: (ctx) =>
      setDeckCtx((have) =>
        have?.label === ctx?.label &&
        have?.text === ctx?.text &&
        have?.selected === ctx?.selected &&
        have?.block === ctx?.block
          ? have
          : ctx && { ...ctx, selected: ctx.selected === true },
      ),
    ask: (words) => {
      if (convId) {
        sendMessage(machine, { conversationId: convId, text: words, mode: 'queue' });
        return;
      }
      sendFirst(words, []);
    },
    fill: (words) => {
      setText(words);
      // A space's view (a call written on the board) opens the chat over it, beside the place; a design's at its edge.
      if (space) {
        callOver();
        return;
      }
      leaveTo('chat');
      setMin(false);
    },
    anchor: (rect) => {
      selection.current = rect;
    },
    bindSection: (el) => {
      sectionEl.current = el;
    },
    // A board's error card sends its error to the design's chat: the chat opens where the person sees it, and the
    // message goes (the design's first one makes the chat).
    report: (words) => {
      leaveTo('chat');
      setMin(false);
      if (convId) {
        sendMessage(machine, { conversationId: convId, text: words, mode: 'queue' });
        return;
      }
      sendFirst(words, []);
    },
    deckChat: (
      <DesignChat
        picked={swap.shown}
        designDraft={designDraft}
        machine={machine}
        creating={creating}
        start={start}
        extra={extra}
        onFirst={sendFirst}
        onOpenChat={openChatPage}
        onPickChat={pickChat}
        open={open}
      />
    ),
    chat: (
      <SiteChat {...chat}>
        <DesignChat
          picked={swap.shown}
          designDraft={designDraft}
          machine={machine}
          creating={creating}
          start={start}
          extra={extra}
          onFirst={sendFirst}
          onOpenChat={openChatPage}
          onPickChat={pickChat}
          open={open}
        />
      </SiteChat>
    ),
    island: (
      <SiteIsland
        field={
          <IslandField
            conv={convId}
            designDraft={designDraft}
            machine={machine}
            creating={creating}
            start={start}
            extra={extra}
            onFirst={sendFirst}
          />
        }
        holding={!empty || (draft?.attachments.length ?? 0) > 0}
        status={status}
        last={convId ? <ConvLast conv={convId} machine={machine} onExpand={expand} /> : null}
        onStop={() => convId && void interrupt(machine, convId)}
        onDismiss={() => setSawRun(false)}
        onExpand={expand}
        min={mode === 'island' && min}
        onMin={setMin}
        mode={mode}
        onZone={onZone}
        notes={notes.length}
      />
    ),
  };
};
