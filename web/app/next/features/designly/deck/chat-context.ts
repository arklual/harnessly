// A deck's context in the chat's field: the slide (and block) the person is on, as one chip that goes with the next
// message as an `<annotation>` block the agent reads. Shared by the site stage and the pane stage. No React here.

export const DECK_CTX_ID = 'deck-context';

export interface IDeckChatContext {
  /** The chip. */
  label: string;
  /** What the agent reads. */
  text: string;
  /** A block is selected (the field asks what to do with it). */
  selected: boolean;
  /** The selected block's template number, when there is one. */
  tpl?: number | null;
  /** `text` is a whole `<annotation>` block already (a video's stretch): it goes as it is, numbered in the message. */
  block?: boolean;
}

const oneLine = (text: string) =>
  text
    .replace(/[\x00-\x1f\x7f]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200)
    .replace(/<(\/?)(annotation)/gi, '<\\$1$2');

/** The block as the agent reads it; read back by features/designly/annotation.ts as a note with the element line. */
export const formatDeckContext = (n: number, text: string): string =>
  [`<annotation n="${n}" kind="deck">`, `element: ${oneLine(text)}`, '</annotation>'].join('\n');

const BLOCK_HEAD = /^<annotation n="\d+"/;

/** The context as the message carries it: a ready block takes the message's number, anything else is the deck's. */
export const formatChatContext = (n: number, ctx: IDeckChatContext): string =>
  ctx.block === true && BLOCK_HEAD.test(ctx.text)
    ? ctx.text.replace(BLOCK_HEAD, `<annotation n="${n}"`)
    : formatDeckContext(n, ctx.text);

/** The chip of the composer. */
export const deckContextQuote = (ctx: IDeckChatContext) => ({
  id: DECK_CTX_ID,
  label: ctx.label,
  title: ctx.label,
  remark: false,
  icon: 'locate',
});
