// The boards of the open document that speak the apps' language: a board (internal/design/texts/client.js) says hello
// (`tk:hl-hello`), is told whether to treat bound elements as the apps' (`tk:hl {on}`) and which elements to outline
// (`tk:hl-marks`), and answers a click on a bound element (`tk:hl-click`). The page believes a message only from a frame
// that said hello from the slot's origin, and addresses its own messages to that origin only.
import {
  boardAt,
  marksOfBoard,
  readHlClick,
  readHlHello,
  type IHlClick,
} from '@/next/features/app-views/sinopia/contributions';
import type { ISinopiaBoard, ISinopiaMark } from '@/next/features/app-views/sinopia/types';

export interface IHlFrame {
  postMessage: (data: unknown, targetOrigin: string) => void;
  closed?: boolean;
}

export interface IHlMessage {
  source: unknown;
  origin: string;
  data: unknown;
}

export type THlEvent =
  | { k: 'hello'; frame: IHlFrame; url: string }
  | { k: 'click'; frame: IHlFrame; click: IHlClick; board: ISinopiaBoard | null };

const isFrame = (v: unknown): v is IHlFrame =>
  typeof v === 'object' && v !== null && typeof (v as IHlFrame).postMessage === 'function';

/** The frames of the boards, by their windows. */
export class HlHub {
  private readonly frames = new Map<IHlFrame, string>();
  private on = false;

  /** `origin` is the slot's (null while there is none: nothing is heard); `setOrigin` follows it. */
  constructor(private origin: string | null = null) {}

  setOrigin(origin: string | null): void {
    this.origin = origin;
  }

  /** Feed it every `message` event of the page window. Returns what it made of one that was a board's. */
  onMessage(event: IHlMessage, boards: readonly ISinopiaBoard[]): THlEvent | null {
    const slot = this.origin;
    if (slot === null || event.origin !== slot || !isFrame(event.source)) return null;
    const hello = readHlHello(event.data);
    if (hello !== null) {
      // A page that names an address of another origin is not a board of the slot.
      if (!sameOrigin(hello, slot)) return null;
      this.frames.set(event.source, hello);
      this.tell(event.source, { type: 'tk:hl', on: this.on });
      return { k: 'hello', frame: event.source, url: hello };
    }
    const click = readHlClick(event.data);
    const url = this.frames.get(event.source);
    // Only a frame that said hello is heard, and what it is is what it said then: the click's own `url` is not trusted.
    if (click === null || url === undefined) return null;
    return { k: 'click', frame: event.source, click, board: boardAt(boards, url) };
  }

  /** Whether the message comes from a board that said hello (from the slot's origin). */
  knows(event: IHlMessage): boolean {
    return this.origin === event.origin && isFrame(event.source) && this.frames.has(event.source);
  }

  /** Turns the apps' treatment of bound elements on or off in every board. */
  setOn(on: boolean): void {
    this.on = on;
    for (const frame of this.live()) this.tell(frame, { type: 'tk:hl', on });
  }

  /** Sends each board the marks of its page. */
  setMarks(marks: readonly ISinopiaMark[], boards: readonly ISinopiaBoard[]): void {
    for (const frame of this.live()) {
      const board = boardAt(boards, this.frames.get(frame) ?? '');
      this.tell(frame, { type: 'tk:hl-marks', items: board ? marksOfBoard(marks, board.id) : [] });
    }
  }

  /** How many boards have said hello and are still there. */
  get size(): number {
    return this.live().length;
  }

  /** The frames that are still there; a window that closed is forgotten. */
  private live(): IHlFrame[] {
    for (const frame of [...this.frames.keys()]) if (frame.closed === true) this.frames.delete(frame);
    return [...this.frames.keys()];
  }

  private tell(frame: IHlFrame, data: unknown): void {
    const slot = this.origin;
    if (slot !== null) frame.postMessage(data, slot);
  }
}

const sameOrigin = (href: string, origin: string): boolean => {
  try {
    return new URL(href).origin === origin;
  } catch {
    return false;
  }
};
