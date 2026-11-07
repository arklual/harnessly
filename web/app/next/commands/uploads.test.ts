// The chunked upload against a runtime that is faked in memory: the same rules as internal/wire/uploads_resumable.go
// (offset = the bytes the machine has, a wrong offset is answered with the right one, nothing is final before complete).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { newUploadSession, uploadFile } from '@/next/commands/uploads';

const CHUNK = 4;

interface IFake {
  size: number;
  have: number;
  done: boolean;
}

let machine: IFake | null;
let plan: ('ok' | 'break' | 'drop' | 'no-space')[];
let calls: string[];

class FakeXhr {
  upload: { onprogress: ((e: { loaded: number }) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  status = 0;
  responseText = '';
  timeout = 0;
  private method = '';
  private headers: Record<string, string> = {};
  open(method: string) {
    this.method = method;
  }
  setRequestHeader(name: string, value: string) {
    this.headers[name.toLowerCase()] = value;
  }
  getResponseHeader(name: string) {
    return name.toLowerCase() === 'upload-offset' && machine ? String(machine.have) : null;
  }
  abort() {
    this.onabort?.();
  }
  send(body: unknown) {
    queueMicrotask(() => this.answer(body));
  }
  private reply(status: number, body: object = {}) {
    this.status = status;
    this.responseText = JSON.stringify(body);
    this.onload?.();
  }
  private answer(body: unknown) {
    calls.push(this.method);
    if (this.method === 'POST' && !machine) {
      const asked = JSON.parse(body as string) as { size: number };
      if (plan[0] === 'no-space') {
        plan.shift();
        this.reply(507, { error: 'no-space', message: 'нет места', need: asked.size, free: 1 });
        return;
      }
      machine = { size: asked.size, have: 0, done: false };
      this.reply(201, { id: 'up_AAAAAAAAAAAAAAAA', offset: 0, chunkSize: CHUNK });
      return;
    }
    if (!machine) {
      this.reply(404, { error: 'no-upload' });
      return;
    }
    if (this.method === 'HEAD') {
      this.reply(200);
      return;
    }
    if (this.method === 'POST') {
      if (machine.have !== machine.size) this.reply(409, { error: 'incomplete', offset: machine.have });
      else
        this.reply(200, {
          id: 'up_AAAAAAAAAAAAAAAA',
          name: 'a.bin',
          kind: 'file',
          mime: 'application/octet-stream',
        });
      return;
    }
    const at = Number(this.headers['upload-offset']);
    if (at !== machine.have) {
      this.reply(409, { error: 'offset-mismatch', offset: machine.have });
      return;
    }
    const chunk = body as Blob;
    const step = plan.shift();
    if (step === 'break' || step === 'drop') {
      if (step === 'break') machine.have += Math.floor(chunk.size / 2); // half of the chunk arrived before the link broke
      this.onerror?.();
      return;
    }
    machine.have += chunk.size;
    this.reply(200, { offset: machine.have });
  }
}

beforeEach(() => {
  machine = null;
  plan = [];
  calls = [];
  vi.stubGlobal('XMLHttpRequest', FakeXhr);
  vi.useFakeTimers({ shouldAdvanceTime: true });
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const file = (size: number) => Object.assign(new Blob([new Uint8Array(size)]), { name: 'a.bin' });

describe('uploadFile', () => {
  it('sends a file in chunks and closes it', async () => {
    const seen: number[] = [];
    const outcome = await uploadFile('m1', file(10), (done) => seen.push(done));
    expect(outcome.ok).toBe(true);
    expect(calls.filter((c) => c === 'PATCH')).toHaveLength(3);
    expect(Math.max(...seen)).toBe(10);
  });

  it('goes on from the offset the machine has after a break in the middle of a chunk', async () => {
    plan = ['ok', 'break'];
    const pending = uploadFile('m1', file(12), () => {});
    await vi.advanceTimersByTimeAsync(5000);
    const outcome = await pending;
    expect(outcome.ok).toBe(true);
    expect(calls).toContain('HEAD');
    expect(machine?.have).toBe(12);
  });

  it('keeps the session when it gives up, and a retry goes on in it', async () => {
    plan = ['ok', ...Array<'drop'>(20).fill('drop')];
    const session = newUploadSession();
    const first = uploadFile('m1', file(12), () => {}, undefined, session);
    await vi.advanceTimersByTimeAsync(10 * 60 * 1000);
    const failed = await first;
    expect(failed).toMatchObject({ ok: false, code: 'network' });
    expect(session.id).toBe('up_AAAAAAAAAAAAAAAA');
    plan = [];
    const outcome = await uploadFile('m1', file(12), () => {}, undefined, session);
    expect(outcome.ok).toBe(true);
    expect(calls.filter((c) => c === 'POST').length).toBe(2); // one create, one complete
  });

  it('refuses a file the disk cannot take, with the numbers', async () => {
    plan = ['no-space'];
    const outcome = await uploadFile('m1', file(10), () => {});
    expect(outcome).toMatchObject({ ok: false, code: 'no-space', need: 10, free: 1 });
  });

  it('stops on cancel', async () => {
    const abort = new AbortController();
    abort.abort();
    expect(await uploadFile('m1', file(10), () => {}, abort.signal)).toEqual({ ok: false, code: 'aborted' });
  });
});
