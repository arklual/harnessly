// The field a card answers an agent with: an agent's question, another agent's message. It is the chat's own
// composer, compact and taking words only — the answer goes back through the card's channel, which carries no files —
// with the voice of the machine the host names (ReplyMachine; bindings/reply-field draws it). The card keeps the words: its other buttons («Одобрить»,
// «Нужны правки») send them too.
import { createContext, type ReactNode, type RefObject } from 'react';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import type { MachineId } from '@/next/protocol/v2';

/** A field of words alone, as the host keeps it: the text, the voice into it, its limit. */
export interface IWordsField {
  text: string;
  setText: (value: string) => void;
  /** What the field holds now, outside a render. */
  read: () => string;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  maxText: number;
  enterSends: boolean;
  /** The voice input as the composer takes it; no microphone where the machine has none. */
  voice: Pick<
    IComposerProps,
    'recording' | 'recognized' | 'caret' | 'onMic' | 'onCancelRecording' | 'onFinishRecording'
  >;
}

export interface IReplyFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  /** The answer is on its way: the field waits. */
  busy: boolean;
  onSend: () => void;
  /** Escape in the field: the card closes its box. */
  onEscape?: () => void;
  /** The host has something to send without words (a picture's marks): the send is offered on an empty field. */
  ready?: boolean;
  inputRef?: RefObject<HTMLTextAreaElement | null>;
  /** What the send button says, where it is not «Отправить». */
  sendLabel?: string;
}

const nothing = () => {};
const noStop = () => Promise.resolve();
const DEFAULT_MAX = 100_000;

/** The composer of a reply, given its field. */
export const ReplyComposer = ({
  value,
  onChange,
  placeholder,
  busy,
  onSend,
  onEscape,
  ready,
  inputRef,
  sendLabel,
  voice,
  maxText = DEFAULT_MAX,
  enterSends = true,
  bar = null,
}: IReplyFieldProps &
  Partial<Pick<IWordsField, 'voice' | 'maxText' | 'enterSends'>> & {
    /** What the voice says over the field (no microphone, no access). */
    bar?: ReactNode;
  }) => (
  <div
    className="hx-dock"
    data-screen
    data-reply
    onKeyDown={(event) => {
      if (event.key !== 'Escape' || !onEscape || event.defaultPrevented) return;
      event.preventDefault();
      onEscape();
    }}
  >
    {bar}
    <Composer
      plain
      value={value}
      onChange={onChange}
      onSubmit={onSend}
      mode="idle"
      quiet={busy}
      placeholder={placeholder}
      onStop={noStop}
      picker={null}
      attachments={[]}
      onFiles={nothing}
      onRemoveAttachment={nothing}
      onRetryAttachment={nothing}
      recording={voice?.recording ?? null}
      onMic={voice?.onMic}
      recognized={voice?.recognized}
      caret={voice?.caret}
      onCancelRecording={voice?.onCancelRecording ?? nothing}
      onFinishRecording={voice?.onFinishRecording ?? nothing}
      maxText={maxText}
      enterSends={enterSends}
      ready={ready}
      inputRef={inputRef}
      sendLabel={sendLabel}
    />
  </div>
);

/** The machine whose voice a card's reply field takes; null where none (the field has no microphone). */
export const ReplyMachine = createContext<MachineId | null>(null);
