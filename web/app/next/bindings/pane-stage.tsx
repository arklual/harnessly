// The stage of a design opened in the right pane. It has no chat of its own: the chat the pane is beside is the one
// that edits the design, so a block picked on the page becomes a note whose chip lands in that chat's field (the notes
// live in stores/design-notes.ts and the field sends them), and the words of the page's tools are added to that
// chat's draft. The page itself changes when the agent writes the board's file.
import { useState } from 'react';
import { NoteCard, useSiteStage, type IStageOptions } from '@/next/bindings/site-stage';
import { anchorItems } from '@/next/features/designly/note-anchor';
import { itemsOf, NO_EDIT } from '@/next/features/designly/element-style';
import type { ISitePaneChat, ISiteStage } from '@/next/features/designly/site-view';
import {
  keepPaneChat,
  PANE_CHAT,
  readPaneChat,
  siteModeOf,
  type TPaneChat,
} from '@/next/features/designly/pane-chat';
import { NotePins, NOTE_OUT_MS } from '@/next/features/designly/site-notes';
import { usePageDraft } from '@/next/features/designly/page-drafts';
import type { ConvId } from '@/next/protocol/v2';
import { DECK_CTX_ID } from '@/next/features/designly/deck/chat-context';
import { sendMessage } from '@/next/commands';
import { patchDesignNotes, useDesignNotes } from '@/next/stores/design-notes';
import { useConversation } from '@/next/stores/conversations';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { PRESENCE, usePresence } from '@/next/ui/presence';

let noteSeq = 0;

/** `fromChat` is the chat the pane is beside: it gets the notes and the words. */
export const usePaneStage = ({ machine, fromChat, pageUrl, port }: IStageOptions): ISiteStage => {
  const conv = fromChat as ConvId;
  // The host chat's agent at work: a deck shows its slides as being made meanwhile.
  const agent = useConversation(conv)?.state.k;
  // A deck's context chip is a note of the chat too, but it has no pin and no card.
  const everything = useDesignNotes(conv);
  const all = everything.filter((note) => note.id !== DECK_CTX_ID);
  // The pins and the looks are of the page on screen; the chips of the other pages stay in the field.
  const notes = all.filter((note) => note.page === pageUrl);
  // The card is the page's: another page hides it, the return brings it back.
  const [noteDraft, setNoteDraft] = usePageDraft(pageUrl);
  const card = usePresence(noteDraft, NOTE_OUT_MS);
  const shown = card.value;
  const [picking, setPicking] = useState(false);
  const [bridged, setBridged] = useState(false);
  // The blocks the page said it no longer has: a look cannot be shown on them.
  const [goneBlocks, setGoneBlocks] = useState<readonly number[]>([]);

  const editNote = (id: string) => {
    const note = notes.find((n) => n.id === id);
    if (!note) return;
    setPicking(false);
    setNoteDraft({ picked: note.picked, at: note.at, id, text: note.text, style: note.style });
  };
  const removeNote = (id: string) => patchDesignNotes(conv, (have) => have.filter((n) => n.id !== id));
  const editing = noteDraft?.id === undefined ? -1 : all.findIndex((n) => n.id === noteDraft.id);
  const noteNumber = editing >= 0 ? editing + 1 : all.length + 1;

  return {
    mode: 'island',
    // The chip rides in the host chat's field as a note on the deck's slide; null takes it away.
    context: (ctx) =>
      patchDesignNotes(conv, (have) => {
        const rest = have.filter((note) => note.id !== DECK_CTX_ID);
        if (!ctx) return rest;
        return [
          ...rest,
          {
            id: DECK_CTX_ID,
            picked: {
              id: null,
              look: null,
              selector: 'slide',
              tag: 'section',
              text: ctx.text,
              rect: { x: 0, y: 0, width: 0, height: 0 },
              component: null,
              html: '',
            },
            page: pageUrl,
            text: '',
            at: { x: 0, y: 0, right: false },
            style: NO_EDIT,
            port: port ?? 0,
          },
        ];
      }),
    ask: (words) => {
      sendMessage(machine, { conversationId: conv, text: words, mode: 'queue' });
    },
    onMode: () => undefined,
    min: false,
    run: 'idle',
    busy: agent === 'starting' || agent === 'working',
    float: null,
    dock: 'left',
    chatW: 0,
    chat: null,
    island: null,
    pick: bridged && noteDraft === null && picking,
    picking: bridged ? picking : null,
    onPick: () => setPicking((on) => !on),
    onPicked: (picked, at) => {
      setPicking(false);
      setNoteDraft({ picked, at, text: '', style: NO_EDIT });
    },
    onPickEnd: () => setPicking(false),
    onBridge: setBridged,
    styles: itemsOf(notes, noteDraft),
    anchors: anchorItems(notes, noteDraft),
    onStyled: setGoneBlocks,
    sview: false,
    onSview: () => undefined,
    // A line of the page's tools lands in the chat's field, after what is typed there.
    onAttach: (words) => {
      const have = draftsStore.get(conv);
      const text = have?.text ?? '';
      setDraft(conv, {
        text: text.trim() === '' ? words : `${text.trimEnd()}\n${words}`,
        attachments: have?.attachments ?? [],
      });
    },
    pins: <NotePins notes={notes} draft={noteDraft} onOpen={editNote} />,
    overlay: shown && (
      <NoteCard
        key={`${shown.id ?? shown.picked.selector}@${shown.at.x},${shown.at.y}`}
        machine={machine}
        n={noteNumber}
        draft={shown}
        hidden={card.state === PRESENCE.closing}
        onTextChange={(text) => setNoteDraft({ ...shown, text })}
        needsText={false}
        gone={shown.picked.id !== null && goneBlocks.includes(shown.picked.id)}
        onStyle={(style) => setNoteDraft({ ...shown, style })}
        onOk={(words) => {
          if (shown.id) {
            patchDesignNotes(conv, (have) =>
              have.map((n) => (n.id === shown.id ? { ...n, text: words, style: shown.style } : n)),
            );
          } else {
            patchDesignNotes(conv, (have) => [
              ...have,
              {
                id: `pn${++noteSeq}`,
                picked: shown.picked,
                page: pageUrl,
                text: words,
                at: shown.at,
                style: shown.style,
                port: port ?? 0,
              },
            ]);
          }
          setNoteDraft(null);
        }}
        onCancel={() => setNoteDraft(null)}
        onDelete={() => {
          if (shown.id) removeNote(shown.id);
          setNoteDraft(null);
        }}
      />
    ),
  };
};

export interface IStageWithChat {
  stage: ISiteStage;
  /** Where the chat stands in the right pane; null on the design's own page. */
  chat: ISitePaneChat | null;
}

/** The design's own page: its stage, no choice of the chat's place. */
export const useSiteOnlyStage = (options: IStageOptions): IStageWithChat => ({
  stage: useSiteStage(options),
  chat: null,
});

/**
 * The right pane's stage with the choice of where the chat stands: «Слева» is the pane's own stage (the notes go to the
 * left chat's field), «Поверх» and «Снизу» are the full view's window and field over the same conversation. Both
 * stages are kept, so a switch keeps what each has.
 */
export const usePaneChatStage = (options: IStageOptions): IStageWithChat => {
  const left = usePaneStage(options);
  const site = useSiteStage(options);
  const [mode, setMode] = useState<TPaneChat>(readPaneChat);
  const want = siteModeOf(mode);
  // The window or the field was dragged into another place (an edge, the pill): the pane's choice follows it; an edge
  // has no room in the pane, so it stays a window.
  const shown = site.mode === 'island' ? PANE_CHAT.bottom : PANE_CHAT.float;
  const chat: ISitePaneChat = {
    mode: want === null ? PANE_CHAT.left : shown,
    onMode: (next, section) => {
      setMode(next);
      keepPaneChat(next);
      const to = siteModeOf(next);
      if (to !== null && to !== site.mode) site.onMode(to, section);
    },
    sync: (section) => {
      if (want !== null && (site.mode === 'chat' || site.mode === 'notes')) site.onMode('float', section);
    },
    start: (section) => {
      if (want !== null && site.mode !== want) site.onMode(want, section);
    },
  };
  return { stage: want === null ? left : site, chat };
};
