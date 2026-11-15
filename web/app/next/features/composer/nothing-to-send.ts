// A message with no words of the person and nothing attached is not sent: only the board's or the page's
// context around an empty line reached the agent three times on 15.11, and it acted on the context alone.

export interface IOutgoing {
  /** What the person typed or said. */
  text: string;
  /** Files, quotes, picked blocks, a file's selection: anything the person put into the message. */
  attached: number;
}

/** True when the message carries nothing of the person's: no words and nothing attached. */
export const nothingToSend = ({ text, attached }: IOutgoing): boolean => text.trim() === '' && attached === 0;
