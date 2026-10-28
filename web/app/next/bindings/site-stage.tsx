// The chat of an open design over the machine. The design's chat is bound to it (bindings/design-conv.ts): it opens at
// once from what the browser kept, and a message goes to that chat, never to another one of the folder. Open beside the
// site (a window or an edge) it is the usual chat — the same feed, field, picker, queue and states as anywhere; folded
// over the site it is the island: the field, and over it the agent's last line with what it does now and «Стоп». A
// design without a chat gets one with its first message, which is in the column from the moment it is sent.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { interrupt, sendMessage } from '@/next/commands';
import { uploadSrc } from '@/next/commands/uploads';
import ChatView from '@/next/bindings/chat-view';
import type { IDockExtra } from '@/next/bindings/chat-dock';
import { outgoingItem, useFeedItems } from '@/next/bindings/feed-items';
import { useAgentLimits } from '@/next/bindings/use-limits';
import { limitsProviderOf } from '@/next/features/limits/model';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { useUploads } from '@/next/bindings/use-uploads';
import { useVoice } from '@/next/bindings/use-voice';
import {
  settleDesignCreating,
  startDesignConv,
  useDesignConv,
  useDesignCreating,
  useDesignSeen,
  type IDesignCreating,
} from '@/next/bindings/design-conv';
import { hub, useConversationChannel } from '@/next/core/realtime/hub';
import Feed from '@/next/features/chat/feed';
import type { FeedItem } from '@/next/features/chat/types';
import type { IModelChoice, IPickerAgent } from '@/next/features/composer/agents';
import Composer from '@/next/features/composer/composer';
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
  type TIslandStatus,
  type TSiteMode,
} from '@/next/features/designly/site-chat';
import type { IFloat, TDock, TZone } from '@/next/features/designly/site-drag';
import type { ISiteStage } from '@/next/features/designly/site-view';
import { formatAnnotation, withAnnotations } from '@/next/features/designly/annotation';
import {
  DECK_CTX_ID,
  deckContextQuote,
  formatDeckContext,
  type IDeckChatContext,
} from '@/next/features/designly/deck/chat-context';
import { anchorItems } from '@/next/features/designly/note-anchor';
import { itemsOf, NO_EDIT } from '@/next/features/designly/element-style';
import {
  NotePins,
  NotePopover,
  noteQuote,
  NotesBar,
  NoteStrip,
  NOTE_OUT_MS,
  type INote,
  type INotePopoverProps,
} from '@/next/features/designly/site-notes';
import { usePageDraft } from '@/next/features/designly/page-drafts';
import type { Conversation, ConvId, MachineId, Now } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { useConnection } from '@/next/stores/connection';
import { useFeed, useNow } from '@/next/stores/conversation';
import { useConversation, useConversationList } from '@/next/stores/conversations';
import { clearDraft, setDraft, useDraft, type DraftKey } from '@/next/stores/drafts';
import { limitsOf, useMachine } from '@/next/stores/machines';
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
  ...props
}: { machine: MachineId; onTextChange?: (text: string) => void } & Omit<
  INotePopoverProps,
  'text' | 'onText' | 'mic'
>) => {
  const [text, setTextHere] = useState(props.draft.text);
  const setText = (value: string) => {
    setTextHere(value);
    onTextChange?.(value);
  };
  const voice = useVoice(machine, text, setText);
  const start = voice.onMic;
  const mic = start && {
    recording: voice.recording !== null,
    onToggle: () => {
      if (!voice.recording) start();
      else if (!voice.recording.busy) voice.onFinishRecording();
    },
  };
  return <NotePopover {...props} text={text} onText={setText} mic={mic ?? null} />;
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

/**
 * A design without a chat: the usual field with the model to start on, over a feed that is empty until the first
 * message, which stands there from the moment it is sent (pending → sent) while its chat is being made.
 */
const NewDesignChat = ({ machine, draftKey, creating, start, extra, onFirst }: INewChatProps) => {
  const enterSends = useEnterSends();
  const me = useMe();
  const connection = useConnection(machine);
  const machineView = useMachine(machine);
  const draft = useDraft(draftKey);
  const text = draft?.text ?? '';
  const files = useMemo(() => draft?.attachments ?? [], [draft]);
  const uploads = useUploads(machine, draftKey, files);
  const onChange = useCallback(
    (value: string) => setDraft(draftKey, { text: value, attachments: files }),
    [draftKey, files],
  );
  const voice = useVoice(machine, text, onChange);
  const limits = useAgentLimits(machine, limitsProviderOf(start.choice));
  const first = useMessage(creating?.clientId ?? null);
  const items = useMemo(() => (first ? [outgoingItem(first)] : []), [first]);
  const [openedAt] = useState(() => Date.now());
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
        dock={
          <Composer
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
        }
      />
    </AttachmentSrc.Provider>
  );
};

interface IDesignChatProps extends Omit<INewChatProps, 'draftKey'> {
  picked: TDesignConv;
  designDraft: DraftKey;
  onOpenChat: () => void;
}

/** The open chat's body: the design's current chat as it is anywhere, or a new one for the design. */
const DesignChat = ({ picked, designDraft, onOpenChat, ...rest }: IDesignChatProps) => {
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
      <ChatView
        key={id}
        conv={id}
        machine={machine}
        onRetryLoad={() => hub.client(machine)?.reconnectNow()}
        onShowBackground={onOpenChat}
        dockExtra={extra}
        pendingItems={pendingItems}
      />
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
  /** The chats of other folders the machine found worked on the design (the library's `touchedBy`). */
  touched: readonly string[];
  title: string;
  /** The page on screen, its path on the library server: its notes are on it. */
  pageUrl: string;
  /** The library server's port; null while the library loads. */
  port: number | null;
  onOpenDrawer: () => void;
}

/** Everything the site view needs to show the chat beside it and the field over it. */
export const useSiteStage = ({
  machine,
  design,
  fromChat,
  dir,
  touched,
  title,
  pageUrl,
  port,
  onOpenDrawer,
}: IStageOptions): ISiteStage => {
  const key = designKey(machine, design);
  const current = useDesignConv(machine, design, fromChat);
  const { picked } = current;
  const list = useConversationList(machine);
  const connection = useConnection(machine);
  const seen = useDesignSeen(key);
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

  const [mode, setMode] = useState<TSiteMode>('island');
  const [min, setMin] = useState(true);
  const [float, setFloat] = useState<IFloat | null>(null);
  const [dock, setDock] = useState<TDock>('left');
  const [chatW, setChatW] = useState(CHAT_WIDTH);
  const [sview, setSview] = useState(false);
  // Where the chat stood open last: the folded one opens there again.
  const [opened, setOpened] = useState<TSiteMode>('chat');

  // One draft for the folded field and the open one: the chat's, or the design's own before it has a chat.
  const designDraft: DraftKey = `design:${key}`;
  const draftKey: DraftKey = convId ?? designDraft;
  const draft = useDraft(draftKey);
  const text = draft?.text ?? '';
  const setText = (value: string) =>
    setDraft(draftKey, { text: value, attachments: draft?.attachments ?? [] });
  const voice = useVoice(machine, text, setText);

  const [notes, setNotes] = useState<INote[]>([]);
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

  const openChatPage = () => convId && goTo({ kind: ROUTE.chat, id: convId });
  const status = statusOf(conv, now, sawRun, creating !== undefined);

  /** The typed words with the notes as the agent reads them. */
  const wrap = (words: string) =>
    withAnnotations(words, [
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
      ...(deckCtx ? [formatDeckContext(notes.length + 1, deckCtx.text)] : []),
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
      key,
      dir,
      choice: start.choice,
      text: words,
      attachments,
      onRefused: (back) => setDraft(designDraft, { text: back, attachments: [] }),
    });
  };

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

  const startVoice = voice.onMic;
  const onMic =
    startVoice &&
    (() => {
      if (!voice.recording) startVoice();
      else if (!voice.recording.busy) voice.onFinishRecording();
    });
  let placeholder = t('next.site.placeholderIslandPlain');
  if (notes.length > 0) placeholder = t('next.notes.placeholderWith');
  else if (bridged) placeholder = t('next.site.placeholderIsland');
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
  // Where the chat was dragged: it settles there with the field open; a pull of the field down folds it.
  const onZone = (zone: TZone, drop: IFloat | null) => {
    if (zone === 'min') {
      setMin(true);
      return;
    }
    if (zone === 'left' || zone === 'right') setDock(zone);
    if (zone === 'float' && drop) setFloat(drop);
    leaveTo(zone === 'left' || zone === 'right' ? 'chat' : zone);
    setMin(false);
  };
  // The folded chat opens beside the site, where it was last (an edge by default).
  const expand = () => {
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
    quotes: [
      ...notes.map((note, i) => noteQuote(note, i + 1)),
      ...(deckCtx ? [deckContextQuote(deckCtx)] : []),
    ],
    placeholder: deckCtx
      ? t(deckCtx.selected ? 'next.deck.chat.placeholderSelected' : 'next.deck.chat.placeholder')
      : undefined,
    onRemoveQuote: (id) => (id === DECK_CTX_ID ? setDeckCtx(null) : removeNote(id)),
    wrap,
    onSent: afterSend,
    tools: pickTool,
  };

  const online = connection === undefined || connection.k === 'live';
  const rows = designChats({ list, dir, seen, touched, current: convId }).map((c) => ({
    id: c.id,
    title: c.title,
    status: chatStatusOf(c, online),
    at: c.lastActivityAt,
  }));
  const chat: ISiteChatProps = {
    title,
    heading: (
      <ChatSwitch
        current={convId}
        title={conv?.title ?? (convId ? title : t('next.site.newDesignChat'))}
        rows={rows}
        onPick={(id) => {
          current.pick(id);
          setEpoch((n) => n + 1);
        }}
      />
    ),
    swapping: swap.leaving,
    mode,
    float,
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
    onOpenChat: convId ? openChatPage : null,
    onOpenSite: () => setSview(true),
  };

  return {
    mode,
    onMode: (next, section) => {
      if (next === mode) return;
      if (next === 'float' && !float) setFloat(firstFloat(section));
      leaveTo(next);
      setMin(next === 'island' && empty);
    },
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
    pins: <NotePins notes={pageNotes} draft={noteDraft} onOpen={editNote} />,
    overlay: (
      <>
        {shown && (
          <NoteCard
            key={`${shown.id ?? shown.picked.selector}@${shown.at.x},${shown.at.y}`}
            machine={machine}
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
    float,
    dock,
    chatW,
    context: (ctx) =>
      setDeckCtx((have) =>
        have?.label === ctx?.label && have?.text === ctx?.text && have?.selected === ctx?.selected
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
        />
      </SiteChat>
    ),
    island: (
      <SiteIsland
        value={text}
        onChange={setText}
        onSend={send}
        canSend={canSend}
        placeholder={placeholder}
        sending={creating !== undefined && !convId}
        status={status}
        last={convId ? <ConvLast conv={convId} machine={machine} onExpand={expand} /> : null}
        onStop={() => convId && void interrupt(machine, convId)}
        onDismiss={() => setSawRun(false)}
        onExpand={expand}
        min={mode === 'island' && min}
        onMin={setMin}
        mode={mode}
        onZone={onZone}
        recording={voice.recording !== null}
        onMic={onMic}
        pick={bridged ? picking : null}
        onPick={onPick}
        notes={notes.length}
        extra={
          <NoteStrip
            notes={notes}
            onEdit={(id) => {
              setMin(false);
              editNote(id);
            }}
            onRemove={removeNote}
            onClear={() => setNotes([])}
          />
        }
      />
    ),
  };
};
