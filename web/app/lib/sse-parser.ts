export interface ISseEvent {
  type: string;
  data: string;
  /** The stream's last event id when this event was dispatched, as `MessageEvent.lastEventId` gives it. */
  lastEventId: string;
}

export interface ISseParser {
  /** Feeds a piece of the stream; complete events go to `onEvent`, a `retry:` field to `onRetry`. */
  push: (chunk: string) => void;
  /** The last event id seen so far, kept across events like an EventSource keeps it. */
  readonly lastEventId: string;
}

/** The event stream format of the HTML spec (§9.2.6), for a stream that does not arrive through an EventSource. */
export const createSseParser = (
  onEvent: (event: ISseEvent) => void,
  onRetry: (ms: number) => void,
  lastEventId = '',
): ISseParser => {
  let buffer = '';
  let type = '';
  let data = '';
  let idBuffer = lastEventId;
  let last = lastEventId;

  const field = (line: string) => {
    if (line === '') {
      last = idBuffer;
      if (data !== '')
        onEvent({
          type: type || 'message',
          data: data.endsWith('\n') ? data.slice(0, -1) : data,
          lastEventId: last,
        });
      type = '';
      data = '';
      return;
    }
    if (line.startsWith(':')) return;
    const colon = line.indexOf(':');
    const name = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? '' : line.slice(colon + 1);
    if (value.startsWith(' ')) value = value.slice(1);
    if (name === 'event') type = value;
    else if (name === 'data') data += `${value}\n`;
    else if (name === 'id') {
      if (!value.includes('\0')) idBuffer = value;
    } else if (name === 'retry' && /^\d+$/.test(value)) onRetry(Number(value));
  };

  return {
    push: (chunk) => {
      buffer += chunk;
      let start = 0;
      for (let i = 0; i < buffer.length; i += 1) {
        const ch = buffer[i];
        if (ch !== '\n' && ch !== '\r') continue;
        // A CR at the very end may be the first half of CRLF: keep it until the next piece.
        if (ch === '\r' && i === buffer.length - 1) break;
        field(buffer.slice(start, i));
        if (ch === '\r' && buffer[i + 1] === '\n') i += 1;
        start = i + 1;
      }
      buffer = buffer.slice(start);
    },
    get lastEventId() {
      return last;
    },
  };
};
