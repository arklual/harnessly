import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  CHUNK_MS,
  ChunkUploader,
  CursorSampler,
  EventBatcher,
  RecSession,
  VoiceFeed,
  pickMime,
  pickVoiceMime,
  type IRecApi,
  type IRecorderLike,
  type IVoiceEvents,
  type IVoiceSocket,
} from './recorder';
import type { IDeckRecording, TDeckEvent } from './types';

const blob = (n = 4) => new Blob([new Uint8Array(n)]);
const noWait = () => Promise.resolve();

class FakeRecorder implements IRecorderLike {
  state: 'inactive' | 'recording' | 'paused' = 'inactive';
  ondataavailable: ((event: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  onerror: ((event: unknown) => void) | null = null;
  slice: number | undefined;
  start(slice?: number) {
    this.state = 'recording';
    this.slice = slice;
  }
  /** Like a MediaRecorder: the last chunk first, then stop. */
  stop() {
    this.state = 'inactive';
    this.ondataavailable?.({ data: blob(2) });
    this.onstop?.();
  }
  pause() {
    this.state = 'paused';
  }
  resume() {
    this.state = 'recording';
  }
  emit(n = 4) {
    this.ondataavailable?.({ data: blob(n) });
  }
}

describe('pickMime', () => {
  it('takes the first the browser can write', () => {
    expect(pickMime(true, (m) => m === 'video/webm')).toBe('video/webm');
    expect(pickMime(true, (m) => m.startsWith('video/mp4'))).toBe('video/mp4');
    expect(pickMime(true, () => false)).toBe('');
  });
  it('only picks containers the runtime takes', () => {
    // a mic-only recording on a browser that writes mp4 only goes as video/mp4, never audio/mp4
    expect(pickMime(false, (m) => m === 'audio/mp4' || m === 'video/mp4')).toBe('video/mp4');
    expect(pickMime(false, (m) => m === 'audio/mp4')).toBe('');
    expect(pickMime(false, (m) => m.startsWith('audio/ogg'))).toBe('audio/ogg;codecs=opus');
  });
  it('lets the recognition take any audio container', () => {
    expect(pickVoiceMime((m) => m === 'audio/mp4')).toBe('audio/mp4');
  });
});

describe('ChunkUploader', () => {
  it('sends in order with sequence numbers from 0', async () => {
    const sent: number[] = [];
    const up = new ChunkUploader(async (seq) => {
      sent.push(seq);
      return { ok: true };
    });
    up.push(blob());
    up.push(blob());
    up.push(blob());
    await up.flush();
    expect(sent).toEqual([0, 1, 2]);
    expect(up.pending).toBe(0);
  });

  it('skips an empty chunk but keeps the numbering', async () => {
    const sent: number[] = [];
    const up = new ChunkUploader(async (seq) => {
      sent.push(seq);
      return { ok: true };
    });
    up.push(blob());
    up.push(new Blob([]));
    up.push(blob());
    await up.flush();
    expect(sent).toEqual([0, 2]);
  });

  it('retries a failed send with a growing pause, then goes on', async () => {
    const waits: number[] = [];
    let calls = 0;
    const up = new ChunkUploader(
      async () => {
        calls += 1;
        if (calls < 3) throw new Error('offline');
        return { ok: true };
      },
      { retryMs: 100, wait: (ms) => (waits.push(ms), Promise.resolve()) },
    );
    up.push(blob());
    await up.flush();
    expect(calls).toBe(3);
    expect(waits).toEqual([100, 200]);
  });

  it('gives up after the retries and says why', async () => {
    const up = new ChunkUploader(
      async () => {
        throw new Error('offline');
      },
      { retries: 2, wait: noWait },
    );
    up.push(blob());
    await expect(up.flush()).rejects.toThrow('offline');
    expect(up.error?.message).toBe('offline');
    up.push(blob());
    await expect(up.flush()).rejects.toThrow('offline');
  });

  it('drops the chunks the runtime already has when it answers next', async () => {
    const sent: number[] = [];
    let first = true;
    const up = new ChunkUploader(async (seq) => {
      sent.push(seq);
      // the runtime has 0 and 1 already (a lost answer), wants 2
      if (first) {
        first = false;
        return { ok: false, next: 2 };
      }
      return { ok: true };
    });
    up.push(blob());
    up.push(blob());
    up.push(blob());
    await up.flush();
    expect(sent).toEqual([0, 2]);
  });

  it('fails when the runtime wants a chunk that is gone', async () => {
    let n = 0;
    const up = new ChunkUploader(async (seq) => {
      n += 1;
      return seq === 1 ? { ok: false, next: 0 } : { ok: true };
    });
    up.push(blob());
    await up.flush();
    up.push(blob());
    await expect(up.flush()).rejects.toThrow(/gone/);
    expect(n).toBe(2);
  });

  it('is idempotent on a repeated flush', async () => {
    const up = new ChunkUploader(async () => ({ ok: true }));
    await up.flush();
    up.push(blob());
    await Promise.all([up.flush(), up.flush()]);
    expect(up.pending).toBe(0);
  });
});

describe('EventBatcher', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());
  const ev = (t: number): TDeckEvent => ({ t, k: 'step', n: t });

  it('sends what it has after the flush time, in one batch', async () => {
    const batches: TDeckEvent[][] = [];
    const b = new EventBatcher(async (lines) => void batches.push(lines));
    b.add(ev(1));
    b.add(ev(2));
    expect(batches).toEqual([]);
    await vi.advanceTimersByTimeAsync(2100);
    expect(batches).toEqual([[ev(1), ev(2)]]);
  });

  it('sends at once when a batch is full', async () => {
    const batches: TDeckEvent[][] = [];
    const b = new EventBatcher(async (lines) => void batches.push(lines), undefined, 3);
    b.add(ev(1));
    b.add(ev(2));
    b.add(ev(3));
    await vi.advanceTimersByTimeAsync(0);
    expect(batches).toHaveLength(1);
    expect(batches[0]).toHaveLength(3);
  });

  it('puts a failed batch back in front and sends it with the next', async () => {
    const batches: TDeckEvent[][] = [];
    let fail = true;
    const b = new EventBatcher(async (lines) => {
      if (fail) throw new Error('down');
      batches.push(lines);
    });
    b.add(ev(1));
    await expect(b.flush()).rejects.toThrow('down');
    expect(b.pending).toBe(1);
    fail = false;
    b.add(ev(2));
    await b.flush();
    expect(batches).toEqual([[ev(1), ev(2)]]);
  });

  it('flush with nothing held sends nothing', async () => {
    const send = vi.fn(async () => {});
    await new EventBatcher(send).flush();
    expect(send).not.toHaveBeenCalled();
  });
});

describe('CursorSampler', () => {
  it('lets ~30 samples a second through', () => {
    const s = new CursorSampler();
    const taken = [0, 10, 20, 34, 40, 70, 80].filter((t) => s.take(t));
    expect(taken).toEqual([0, 34, 70]);
  });
});

describe('RecSession', () => {
  let now = 0;
  let rec: FakeRecorder;
  let chunks: [string, number][];
  let lines: TDeckEvent[];
  let finished: [string, number] | null;
  const api = (): IRecApi => ({
    start: async () => 'rec1',
    chunk: async (id, seq) => (chunks.push([id, seq]), { ok: true }),
    events: async (_id, l) => void lines.push(...l),
    finish: async (id, ms) => {
      finished = [id, ms];
      return { summary: { id } } as unknown as IDeckRecording;
    },
  });
  const session = () =>
    new RecSession({
      api: api(),
      stream: {} as MediaStream,
      camera: true,
      mime: 'video/webm',
      makeRecorder: () => (rec = new FakeRecorder()),
      clock: () => now,
      wait: noWait,
      onError: () => {},
    });

  beforeEach(() => {
    now = 1000;
    chunks = [];
    lines = [];
    finished = null;
  });

  it('records in 5 s chunks and uploads them in order', async () => {
    const s = session();
    expect(await s.begin()).toBe('rec1');
    expect(rec.slice).toBe(CHUNK_MS);
    rec.emit();
    rec.emit();
    now += 12_000;
    const result = await s.finish();
    expect(chunks).toEqual([
      ['rec1', 0],
      ['rec1', 1],
      ['rec1', 2],
    ]);
    expect(finished).toEqual(['rec1', 12_000]);
    expect(result.summary.id).toBe('rec1');
  });

  it('stamps events with the media time, not counting the pause', async () => {
    const s = session();
    await s.begin();
    now += 3000;
    s.event({ k: 'slide', page: 'p1', i: 0 });
    s.pause();
    expect(rec.state).toBe('paused');
    now += 10_000;
    s.event({ k: 'retake', page: 'p1' });
    s.resume();
    expect(rec.state).toBe('recording');
    now += 2000;
    s.event({ k: 'step', n: 1 });
    await s.finish();
    expect(lines.map((l) => [l.k, l.t])).toEqual([
      ['slide', 3000],
      ['pause', 3000],
      ['retake', 3000],
      ['resume', 3000],
      ['step', 5000],
    ]);
    expect(finished?.[1]).toBe(5000);
  });

  it('finishing while paused counts only the time before the pause', async () => {
    const s = session();
    await s.begin();
    now += 4000;
    s.pause();
    now += 9000;
    await s.finish();
    expect(finished?.[1]).toBe(4000);
  });

  it('throws from finish when a chunk could not be sent', async () => {
    const bad: IRecApi = {
      ...api(),
      chunk: async () => {
        throw new Error('offline');
      },
    };
    const s = new RecSession({
      api: bad,
      stream: {} as MediaStream,
      camera: false,
      mime: 'audio/webm',
      makeRecorder: () => (rec = new FakeRecorder()),
      clock: () => now,
      wait: noWait,
      onError: () => {},
    });
    await s.begin();
    rec.emit();
    await expect(s.finish()).rejects.toThrow('offline');
    expect(finished).toBeNull();
  });

  it('ignores events after the end', async () => {
    const s = session();
    await s.begin();
    await s.finish();
    s.event({ k: 'step', n: 9 });
    expect(lines.find((l) => l.k === 'step')).toBeUndefined();
  });
});

describe('VoiceFeed', () => {
  class FakeSocket implements IVoiceSocket {
    sent: number[] = [];
    stopped = false;
    constructor(readonly events: IVoiceEvents) {}
    send(chunk: Blob) {
      this.sent.push(chunk.size);
    }
    stop() {
      this.stopped = true;
    }
    abort() {}
  }
  let now = 0;
  let sockets: FakeSocket[];
  let recorders: FakeRecorder[];
  let partials: string[];
  let finals: [string, number][];
  let failures: boolean[];
  let rotate: (() => void) | null;
  const feed = () =>
    new VoiceFeed({
      audio: {} as MediaStream,
      mime: 'audio/webm',
      makeRecorder: () => {
        const r = new FakeRecorder();
        recorders.push(r);
        return r;
      },
      open: (events) => {
        const s = new FakeSocket(events);
        sockets.push(s);
        return s;
      },
      now: () => now,
      schedule: (run) => {
        rotate = run;
        return () => {
          if (rotate === run) rotate = null;
        };
      },
      onPartial: (t) => partials.push(t),
      onFinal: (t, t0) => finals.push([t, t0]),
      onFailure: (f) => failures.push(f.unavailable),
    });

  beforeEach(() => {
    now = 0;
    sockets = [];
    recorders = [];
    partials = [];
    finals = [];
    failures = [];
    rotate = null;
  });

  it('holds the audio until the recognition is ready, then sends it all in order', () => {
    const f = feed();
    f.start();
    recorders[0]!.emit(1);
    recorders[0]!.emit(2);
    expect(sockets[0]!.sent).toEqual([]);
    sockets[0]!.events.onReady();
    expect(sockets[0]!.sent).toEqual([1, 2]);
    recorders[0]!.emit(3);
    expect(sockets[0]!.sent).toEqual([1, 2, 3]);
    expect(recorders[0]!.slice).toBe(250);
  });

  it('reports the partials and the final with the time of the first partial', () => {
    const f = feed();
    f.start();
    sockets[0]!.events.onReady();
    now = 1500;
    sockets[0]!.events.onPartial('привет');
    now = 2600;
    sockets[0]!.events.onPartial('привет мир');
    now = 3000;
    sockets[0]!.events.onFinal(' привет мир ');
    expect(partials).toEqual(['привет', 'привет мир']);
    expect(finals).toEqual([['привет мир', 1500]]);
  });

  it('rotates: the old stretch gets its final words, the new one takes over at once', () => {
    const f = feed();
    f.start();
    sockets[0]!.events.onReady();
    f.rotate();
    expect(recorders[0]!.state).toBe('inactive');
    expect(sockets[0]!.stopped).toBe(true);
    expect(sockets).toHaveLength(2);
    // the old stretch's late partial no longer moves the prompter; its final is kept
    sockets[0]!.events.onPartial('старое');
    sockets[0]!.events.onFinal('старое слово');
    sockets[1]!.events.onPartial('новое');
    expect(partials).toEqual(['новое']);
    expect(finals.map((x) => x[0])).toEqual(['старое слово']);
  });

  it('rotates on its own timer', () => {
    const f = feed();
    f.start();
    expect(rotate).not.toBeNull();
    rotate?.();
    expect(sockets).toHaveLength(2);
  });

  it('stands still while paused and goes on after', () => {
    const f = feed();
    f.start();
    f.pause();
    expect(sockets[0]!.stopped).toBe(true);
    expect(sockets).toHaveLength(1);
    f.rotate();
    expect(sockets).toHaveLength(1);
    f.resume();
    expect(sockets).toHaveLength(2);
  });

  it('stop closes the stretch and opens none', () => {
    const f = feed();
    f.start();
    f.stop();
    expect(sockets[0]!.stopped).toBe(true);
    expect(sockets).toHaveLength(1);
  });

  it('tells when the machine cannot recognise, and does not retry', () => {
    const f = feed();
    f.start();
    sockets[0]!.events.onFailure({ message: '', unavailable: true });
    expect(failures).toEqual([true]);
    expect(sockets).toHaveLength(1);
  });

  it('reopens once after a dropped socket, then tells', () => {
    const f = feed();
    f.start();
    sockets[0]!.events.onLost();
    expect(sockets).toHaveLength(2);
    expect(failures).toEqual([]);
    sockets[1]!.events.onLost();
    expect(failures).toEqual([false]);
    expect(sockets).toHaveLength(2);
  });
});
