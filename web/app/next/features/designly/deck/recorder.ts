// The recording of a talk, without React: one MediaRecorder on the camera and the microphone cutting 5-second chunks, an
// uploader that sends them in order with a sequence number (retrying, and answering the runtime's «next» when it lost
// one), a batcher of the events of the show, and a second, audio-only recording whose chunks go to the machine's speech
// recognition so the prompter can follow the voice. Everything the browser provides comes in as an argument, so the tests
// run it with fakes.
import type { IDeckRecording, TDeckEvent } from '@/next/features/designly/deck/types';
import { Stopwatch } from '@/next/features/designly/deck/clock';

/** The video chunks are this long: the runtime appends them to the file, a lost tab loses at most this much. */
export const CHUNK_MS = 5000;
/** The recognition gets audio this often: the sooner, the sooner the prompter moves. */
export const VOICE_SLICE_MS = 250;
/** A recognition stretch is closed and a new one opened this often, so a final comes and the text stays short. */
export const VOICE_ROTATE_MS = 25_000;
export const EVENTS_FLUSH_MS = 2000;
export const EVENTS_BATCH_MAX = 50;
export const CURSOR_MIN_GAP_MS = 33;
export const UPLOAD_RETRIES = 5;
export const UPLOAD_RETRY_MS = 600;

/** The part of MediaRecorder the code uses. */
export interface IRecorderLike {
  readonly state: 'inactive' | 'recording' | 'paused';
  ondataavailable: ((event: { data: Blob }) => void) | null;
  onstop: (() => void) | null;
  onerror: ((event: unknown) => void) | null;
  start: (timeslice?: number) => void;
  stop: () => void;
  pause: () => void;
  resume: () => void;
}
export type TMakeRecorder = (stream: MediaStream, mime: string) => IRecorderLike;

/**
 * The containers the runtime takes for a recording (video/webm, video/mp4, audio/webm, audio/ogg, codecs allowed), best
 * first. A microphone alone on a browser that writes only mp4 records that as video/mp4.
 */
const VIDEO_MIMES = ['video/webm;codecs=vp9,opus', 'video/webm;codecs=vp8,opus', 'video/webm', 'video/mp4'];
const AUDIO_MIMES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg;codecs=opus', 'video/mp4'];
/** What the recognition is fed with never leaves the page, so any audio container does. */
const VOICE_MIMES = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', 'audio/aac'];

/** '' when the browser has no recorder the runtime can take (the recording is not started). */
export const pickMime = (video: boolean, supported: (mime: string) => boolean): string =>
  (video ? VIDEO_MIMES : AUDIO_MIMES).find((m) => supported(m)) ?? '';

/** The container for the audio sent to the speech recognition. */
export const pickVoiceMime = (supported: (mime: string) => boolean): string =>
  VOICE_MIMES.find((m) => supported(m)) ?? '';

// ---- chunks

export type TSendChunk = (seq: number, chunk: Blob) => Promise<{ ok: true } | { ok: false; next: number }>;

export interface IUploaderOptions {
  retries?: number;
  retryMs?: number;
  wait?: (ms: number) => Promise<void>;
}

/**
 * Sends the chunks in order, numbered from 0. A failed send is retried with a growing pause; the runtime answering that
 * it already has chunks up to `next` drops them from the queue, and answering with one the uploader no longer holds is a
 * gap that cannot be mended. The first unmendable failure is final: `flush()` rejects with it.
 */
export class ChunkUploader {
  private queue: { seq: number; chunk: Blob }[] = [];
  private seq = 0;
  private running: Promise<void> | null = null;
  private failure: Error | null = null;
  private readonly retries: number;
  private readonly retryMs: number;
  private readonly wait: (ms: number) => Promise<void>;

  constructor(
    private readonly send: TSendChunk,
    options: IUploaderOptions = {},
  ) {
    this.retries = options.retries ?? UPLOAD_RETRIES;
    this.retryMs = options.retryMs ?? UPLOAD_RETRY_MS;
    this.wait = options.wait ?? ((ms) => new Promise((resolve) => setTimeout(resolve, ms)));
  }

  /** Chunks not yet acknowledged. */
  get pending(): number {
    return this.queue.length;
  }

  get error(): Error | null {
    return this.failure;
  }

  push(chunk: Blob): number {
    const seq = this.seq;
    this.seq += 1;
    if (chunk.size === 0) return seq;
    this.queue.push({ seq, chunk });
    this.pump();
    return seq;
  }

  /** Resolves when everything pushed is acknowledged; rejects with the failure that stopped the queue. */
  async flush(): Promise<void> {
    this.pump();
    while (this.running) await this.running;
    if (this.failure) throw this.failure;
  }

  private pump(): void {
    if (this.running || this.failure || this.queue.length === 0) return;
    this.running = this.drain().finally(() => {
      this.running = null;
    });
  }

  private async drain(): Promise<void> {
    let tries = 0;
    for (;;) {
      const head = this.queue[0];
      if (!head) return;
      let answer: Awaited<ReturnType<TSendChunk>>;
      try {
        answer = await this.send(head.seq, head.chunk);
      } catch (error) {
        tries += 1;
        if (tries > this.retries) {
          this.failure = error instanceof Error ? error : new Error('chunk upload failed');
          return;
        }
        await this.wait(this.retryMs * 2 ** (tries - 1));
        continue;
      }
      if (answer.ok) {
        this.queue.shift();
        tries = 0;
        continue;
      }
      // the runtime wants chunk `next`: everything before it it already has
      if (answer.next > head.seq) {
        this.queue = this.queue.filter((item) => item.seq >= answer.next);
        tries = 0;
        continue;
      }
      this.failure = new Error(`the runtime wants chunk ${answer.next}, which is gone`);
      return;
    }
  }
}

// ---- events

export type TSendEvents = (lines: TDeckEvent[]) => Promise<void>;
export type TSchedule = (run: () => void, ms: number) => () => void;

const defaultSchedule: TSchedule = (run, ms) => {
  const id = setTimeout(run, ms);
  return () => clearTimeout(id);
};

/** Collects the events and sends them in batches: every couple of seconds, or at once when a batch is full. */
export class EventBatcher {
  private lines: TDeckEvent[] = [];
  private cancel: (() => void) | null = null;
  private inflight: Promise<void> = Promise.resolve();
  private failure: Error | null = null;

  constructor(
    private readonly send: TSendEvents,
    private readonly schedule: TSchedule = defaultSchedule,
    private readonly max = EVENTS_BATCH_MAX,
    private readonly flushMs = EVENTS_FLUSH_MS,
  ) {}

  get pending(): number {
    return this.lines.length;
  }

  add(event: TDeckEvent): void {
    this.lines.push(event);
    if (this.lines.length >= this.max) {
      void this.flush().catch(() => {});
      return;
    }
    if (!this.cancel) {
      this.cancel = this.schedule(() => {
        this.cancel = null;
        void this.flush().catch(() => {});
      }, this.flushMs);
    }
  }

  /** Sends what is held. A batch that failed goes back in front and the failure is thrown (the next flush tries again). */
  flush(): Promise<void> {
    this.cancel?.();
    this.cancel = null;
    this.inflight = this.inflight.then(async () => {
      if (this.lines.length === 0) return;
      const batch = this.lines;
      this.lines = [];
      try {
        await this.send(batch);
        this.failure = null;
      } catch (error) {
        this.lines = [...batch, ...this.lines];
        this.failure = error instanceof Error ? error : new Error('events upload failed');
        throw this.failure;
      }
    });
    // a failed flush must not poison the chain for the next one
    const result = this.inflight;
    this.inflight = result.catch(() => {});
    return result;
  }
}

/** Lets a pointer position through at most ~30 times a second. */
export class CursorSampler {
  private last = Number.NEGATIVE_INFINITY;
  constructor(private readonly gapMs = CURSOR_MIN_GAP_MS) {}
  take(t: number): boolean {
    if (t - this.last < this.gapMs) return false;
    this.last = t;
    return true;
  }
}

// ---- the video recorder

/** Records a stream in chunks of `CHUNK_MS` into the uploader; `stop()` resolves after the last chunk is handed over. */
export class MediaChunker {
  private rec: IRecorderLike | null = null;
  private stopped: Promise<void> | null = null;

  constructor(
    private readonly make: TMakeRecorder,
    private readonly uploader: ChunkUploader,
    private readonly onError: (error: unknown) => void,
  ) {}

  start(stream: MediaStream, mime: string): void {
    const rec = this.make(stream, mime);
    rec.ondataavailable = (event) => {
      if (event.data.size > 0) this.uploader.push(event.data);
    };
    rec.onerror = this.onError;
    rec.start(CHUNK_MS);
    this.rec = rec;
  }

  pause(): void {
    if (this.rec?.state === 'recording') this.rec.pause();
  }

  resume(): void {
    if (this.rec?.state === 'paused') this.rec.resume();
  }

  stop(): Promise<void> {
    const rec = this.rec;
    if (!rec || rec.state === 'inactive') return this.stopped ?? Promise.resolve();
    this.stopped = new Promise((resolve) => {
      rec.onstop = () => resolve();
    });
    rec.stop();
    return this.stopped;
  }
}

// ---- the voice

/** The speech recognition's side (features/composer/voice-socket.ts), as the feed uses it. */
export interface IVoiceSocket {
  send: (chunk: Blob) => void;
  stop: () => void;
  abort: () => void;
}
export interface IVoiceEvents {
  onReady: () => void;
  onPartial: (text: string) => void;
  onFinal: (text: string) => void;
  onFailure: (failure: { message: string; unavailable: boolean }) => void;
  onLost: () => void;
}

export interface IVoiceFeedDeps {
  /** The microphone alone; the feed records it for the recognition and never touches the video's recording. */
  audio: MediaStream;
  mime: string;
  makeRecorder: TMakeRecorder;
  open: (events: IVoiceEvents) => IVoiceSocket;
  /** The recording's clock, ms. */
  now: () => number;
  schedule?: TSchedule;
  rotateMs?: number;
  /** The newest stretch's whole text so far. */
  onPartial: (text: string) => void;
  /** A finished phrase: `t0` is when its first partial came. */
  onFinal: (text: string, t0: number) => void;
  /** The recognition cannot run (no model on the machine, a dropped socket): the prompter goes by hand. */
  onFailure: (failure: { message: string; unavailable: boolean }) => void;
}

interface IStretch {
  socket: IVoiceSocket;
  rec: IRecorderLike;
  ready: boolean;
  held: Blob[];
  t0: number | null;
  startedAt: number;
  closed: boolean;
}

/**
 * Feeds the microphone to the recognition in stretches. A stretch ends on `rotate()` (the slide changed, a pause, every
 * 25 s) or at the end; the recognition answers its last words as a final, which is reported with the time of its first
 * partial. The next stretch opens at once, so no speech falls between two. Only the newest stretch moves the prompter.
 */
export class VoiceFeed {
  private current: IStretch | null = null;
  private cancelRotate: (() => void) | null = null;
  private running = false;
  private readonly schedule: TSchedule;
  private readonly rotateMs: number;
  private reopened = 0;

  constructor(private readonly deps: IVoiceFeedDeps) {
    this.schedule = deps.schedule ?? defaultSchedule;
    this.rotateMs = deps.rotateMs ?? VOICE_ROTATE_MS;
  }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.open();
  }

  /** Closes the stretch that runs and opens the next one. */
  rotate(): void {
    if (!this.running) return;
    this.closeCurrent();
    this.open();
  }

  /** No recognition while the recording stands still. */
  pause(): void {
    if (!this.running) return;
    this.running = false;
    this.closeCurrent();
  }

  resume(): void {
    this.start();
  }

  stop(): void {
    this.running = false;
    this.closeCurrent();
  }

  private open(): void {
    const stretch: IStretch = {
      socket: null as unknown as IVoiceSocket,
      rec: null as unknown as IRecorderLike,
      ready: false,
      held: [],
      t0: null,
      startedAt: this.deps.now(),
      closed: false,
    };
    const isCurrent = () => this.current === stretch;
    stretch.socket = this.deps.open({
      onReady: () => {
        stretch.ready = true;
        for (const chunk of stretch.held) stretch.socket.send(chunk);
        stretch.held = [];
      },
      onPartial: (text) => {
        if (stretch.t0 === null) stretch.t0 = this.deps.now();
        if (isCurrent()) this.deps.onPartial(text);
      },
      onFinal: (text) => {
        const t0 = stretch.t0 ?? stretch.startedAt;
        if (text.trim() !== '') this.deps.onFinal(text.trim(), t0);
      },
      onFailure: (failure) => this.fail(stretch, failure),
      onLost: () => this.fail(stretch, { message: '', unavailable: false }),
    });
    const rec = this.deps.makeRecorder(this.deps.audio, this.deps.mime);
    rec.ondataavailable = (event) => {
      if (event.data.size === 0) return;
      if (stretch.ready) stretch.socket.send(event.data);
      else stretch.held.push(event.data);
    };
    // the recorder's last chunk comes before its stop: only then the recognition is asked for its final words
    rec.onstop = () => stretch.socket.stop();
    rec.start(VOICE_SLICE_MS);
    stretch.rec = rec;
    this.current = stretch;
    this.cancelRotate?.();
    this.cancelRotate = this.schedule(() => this.rotate(), this.rotateMs);
  }

  private closeCurrent(): void {
    this.cancelRotate?.();
    this.cancelRotate = null;
    const stretch = this.current;
    this.current = null;
    if (!stretch || stretch.closed) return;
    stretch.closed = true;
    if (stretch.rec.state !== 'inactive') stretch.rec.stop();
    else stretch.socket.stop();
  }

  private fail(stretch: IStretch, failure: { message: string; unavailable: boolean }): void {
    if (stretch.closed) return;
    stretch.closed = true;
    if (stretch.rec.state !== 'inactive') {
      stretch.rec.onstop = null;
      stretch.rec.stop();
    }
    if (this.current !== stretch) return;
    this.current = null;
    // a dropped socket gets one more try; a machine that cannot recognise stays silent
    if (this.running && !failure.unavailable && this.reopened < 1) {
      this.reopened += 1;
      this.open();
      return;
    }
    this.running = false;
    this.cancelRotate?.();
    this.cancelRotate = null;
    this.deps.onFailure(failure);
  }
}

// ---- the session

export interface IRecApi {
  start: (camera: boolean, mime: string) => Promise<string>;
  chunk: (rec: string, seq: number, chunk: Blob) => ReturnType<TSendChunk>;
  events: (rec: string, lines: TDeckEvent[]) => Promise<void>;
  finish: (rec: string, durationMs: number) => Promise<IDeckRecording>;
}

export interface IRecSessionDeps {
  api: IRecApi;
  /** Camera and microphone, or the microphone alone. */
  stream: MediaStream;
  camera: boolean;
  mime: string;
  makeRecorder: TMakeRecorder;
  /** A monotonic clock, ms. */
  clock: () => number;
  schedule?: TSchedule;
  wait?: (ms: number) => Promise<void>;
  /** The video's recording failed for good (the browser's recorder or the uploads). */
  onError: (error: unknown) => void;
}

/** An event as the page makes it, before the recording stamps its time. */
export type TEventBody = TDeckEvent extends infer E ? (E extends unknown ? Omit<E, 't'> : never) : never;

/**
 * One recording from start to finish. `event()` stamps the events with the recording's own time (the time paused is not
 * counted: it is the time of the media, which the runtime renders against).
 */
export class RecSession {
  private readonly watch = new Stopwatch();
  private readonly uploader: ChunkUploader;
  private readonly chunker: MediaChunker;
  private batcher: EventBatcher | null = null;
  private rec: string | null = null;
  private over = false;

  constructor(private readonly deps: IRecSessionDeps) {
    this.uploader = new ChunkUploader((seq, chunk) => deps.api.chunk(this.id(), seq, chunk), {
      wait: deps.wait,
    });
    this.chunker = new MediaChunker(deps.makeRecorder, this.uploader, deps.onError);
  }

  private id(): string {
    if (!this.rec) throw new Error('the recording has not started');
    return this.rec;
  }

  /** Asks the runtime for a recording and starts the recorder. */
  async begin(): Promise<string> {
    this.rec = await this.deps.api.start(this.deps.camera, this.deps.mime);
    const rec = this.rec;
    this.batcher = new EventBatcher((lines) => this.deps.api.events(rec, lines), this.deps.schedule);
    this.watch.start(this.deps.clock());
    this.chunker.start(this.deps.stream, this.deps.mime);
    return rec;
  }

  get recId(): string | null {
    return this.rec;
  }

  get paused(): boolean {
    return this.watch.paused;
  }

  /** Milliseconds of the recording so far. */
  ms(): number {
    return this.watch.elapsed(this.deps.clock());
  }

  /** Chunks the runtime has not yet acknowledged, and the uploader's failure, for a warning on the screen. */
  get backlog(): number {
    return this.uploader.pending;
  }

  get uploadError(): Error | null {
    return this.uploader.error;
  }

  event(body: TEventBody): void {
    if (this.over || !this.batcher) return;
    this.batcher.add({ ...body, t: this.ms() } as TDeckEvent);
  }

  pause(): void {
    if (this.over || this.watch.paused) return;
    this.chunker.pause();
    this.event({ k: 'pause' });
    this.watch.pause(this.deps.clock());
  }

  resume(): void {
    if (this.over || !this.watch.paused) return;
    this.watch.resume(this.deps.clock());
    this.chunker.resume();
    this.event({ k: 'resume' });
  }

  /** Stops, makes sure the runtime has every chunk and every event, and closes the recording. */
  async finish(): Promise<IDeckRecording> {
    const rec = this.id();
    if (this.watch.paused) this.watch.resume(this.deps.clock());
    const durationMs = Math.round(this.ms());
    this.over = true;
    await this.chunker.stop();
    await this.uploader.flush();
    await this.batcher?.flush();
    return this.deps.api.finish(rec, durationMs);
  }
}
