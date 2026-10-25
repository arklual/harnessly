// A fake v2 runtime for tests: the socket (hello → welcome, sub with resume, rings per channel), the commands
// with the opId store, and the HTTP pages — seeded with the contract's fixtures (internal/wire/testdata). Every
// frame it sends goes through the strict parser on the page side, so a fake that drifts from the contract fails.

import {
  parseClientFrame,
  parseCommand,
  RETRYABLE_REFUSE_CODES,
  type Channel,
  type ClientFrame,
  type CmdResult,
  type Command,
  type ConvChannel,
  type ConvSnapshot,
  type Conversation,
  type ConvsSnapshot,
  type Draft,
  type EntryId,
  type Event,
  type Machine,
  type Op,
  type ServerFrame,
} from '@/next/protocol/v2';
import type { SocketLike } from '@/next/core/realtime/client';

const fixtures = import.meta.glob<unknown>('../../../../../internal/wire/testdata/*.json', {
  eager: true,
  import: 'default',
});

/** A fixture by its name (without .json), deep-copied. */
export function fixture<T = Record<string, unknown>>(name: string): T {
  const hit = Object.entries(fixtures).find(([p]) => p.endsWith(`/${name}.json`));
  if (!hit) throw new Error(`no fixture ${name}`);
  return JSON.parse(JSON.stringify(hit[1])) as T;
}

export const fixtureNames = (): string[] =>
  Object.keys(fixtures).map((p) =>
    p
      .split('/')
      .pop()!
      .replace(/\.json$/, ''),
  );

type Welcome = Extract<ServerFrame, { f: 'welcome' }>;

export class FakeSocket implements SocketLike {
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  readonly url: string;
  readonly sent: ClientFrame[] = [];
  closed = false;
  /** A silent socket: frames from the runtime stop arriving, nothing closes (a dead link the browser has not seen). */
  silent = false;
  subs = new Set<Channel>();
  private readonly rt: FakeRuntime;

  constructor(url: string, rt: FakeRuntime) {
    this.url = url;
    this.rt = rt;
  }

  send(data: string): void {
    if (this.closed) throw new Error('send on a closed socket');
    const frame = parseClientFrame(JSON.parse(data));
    this.sent.push(frame);
    this.rt.onClientFrame(this, frame);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.rt.forget(this);
  }

  /** The runtime side: a frame to the page. */
  deliver(frame: ServerFrame): void {
    if (this.closed || this.silent) return;
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  /** The runtime side closes it (restart, network). */
  serverClose(): void {
    if (this.closed) return;
    this.closed = true;
    this.rt.forget(this);
    this.onclose?.({});
  }
}

interface Ring {
  seq: number;
  events: { seq: number; e: Event }[];
}

type Handler = (cmd: Command, rt: FakeRuntime) => CmdResult | Promise<CmdResult>;

export interface FakeRuntimeOptions {
  machine?: string;
  /** Socket opens are async like a browser's; tests flush with await. */
  conversations?: Conversation[];
  conv?: Record<string, ConvSnapshot>;
}

export class FakeRuntime {
  readonly machineId: string;
  epoch = 'ep-01JB7Q0000';
  welcome: Welcome;
  machine: Machine;
  conversations: Conversation[];
  convs: Map<string, ConvSnapshot>;
  rings = new Map<Channel, Ring>();
  sockets: FakeSocket[] = [];
  opened: FakeSocket[] = [];
  /** New sockets fail at once (machine down, network gone). */
  down = false;
  /** Answers to commands by op; the default for send queues the message and shows it on the conv channel. */
  handlers = new Map<Op, Handler>();
  ops = new Map<string, { body: string; result: CmdResult }>();
  commands: Command[] = [];
  /** The next n commands get the gateway's 502 page: the machine restarts behind it. */
  gatewayErrors = 0;
  /** Commands hang without an answer (the fetch never resolves) while set. */
  hang = false;
  /** How /hello answers: v2, not at all (network), or as an older runtime does — its page, not v2's JSON. */
  hello: 'v2' | 'network' | 'older' = 'v2';
  /** The next n commands are done by the runtime but their answers are lost on the way back. */
  loseAnswers = 0;
  /** How long a request takes to reach the runtime, on the (fake) timers. */
  latencyMs = 0;
  /** Ring size: events older than this many are gone, a resume then needs a snapshot. */
  ringSize = 2000;
  /** The drafts the convs snapshot carries (a runtime with `drafts`). */
  drafts: Draft[] = [];
  private nextConv = 1;

  constructor(o: FakeRuntimeOptions = {}) {
    this.welcome = fixture<Welcome>('frame.server.welcome');
    this.machineId = o.machine ?? this.welcome.machine.id;
    this.machine = { ...this.welcome.machine, id: this.machineId };
    const convs = fixture<{ data: ConvsSnapshot }>('snapshot.convs').data.conversations;
    this.conversations = (o.conversations ?? convs).map((c) => ({ ...c, machineId: this.machineId }));
    const conv = fixture<{ ch: ConvChannel; data: ConvSnapshot }>('snapshot.conv');
    this.convs = new Map(Object.entries(o.conv ?? { [conv.ch.slice(5)]: conv.data }));
    this.handlers.set('send', (cmd, rt) => rt.defaultSend(cmd as Extract<Command, { op: 'send' }>));
    this.handlers.set('conversation.create', (cmd, rt) =>
      rt.defaultCreate(cmd as Extract<Command, { op: 'conversation.create' }>),
    );
  }

  // ---- socket

  socket = (url: string): SocketLike => {
    const s = new FakeSocket(url, this);
    this.opened.push(s);
    if (this.down) {
      queueMicrotask(() => s.serverClose());
      return s;
    }
    this.sockets.push(s);
    queueMicrotask(() => {
      if (!s.closed) s.onopen?.({});
    });
    return s;
  };

  forget(s: FakeSocket): void {
    this.sockets = this.sockets.filter((x) => x !== s);
  }

  onClientFrame(s: FakeSocket, f: ClientFrame): void {
    switch (f.f) {
      case 'hello':
        s.deliver({ ...this.welcome, epoch: this.epoch, machine: this.machine });
        return;
      case 'ping':
        s.deliver({ f: 'pong', t: f.t });
        return;
      case 'unsub':
        s.subs.delete(f.ch);
        return;
      case 'sub':
        this.onSub(s, f);
    }
  }

  private onSub(s: FakeSocket, f: Extract<ClientFrame, { f: 'sub' }>): void {
    if (f.ch.startsWith('conv:') && !this.convs.has(f.ch.slice(5))) {
      s.deliver({ f: 'error', ch: f.ch, code: 'not-found' });
      return;
    }
    s.subs.add(f.ch);
    const ring = this.ring(f.ch);
    // Subscribed before the snapshot is taken: nothing falls between them (wire decision 21).
    if (f.epoch === this.epoch && f.afterSeq !== undefined) {
      const oldest = ring.events[0]?.seq ?? ring.seq + 1;
      if (f.afterSeq >= oldest - 1 && f.afterSeq <= ring.seq) {
        for (const ev of ring.events) if (ev.seq > f.afterSeq) this.deliverEv(s, f.ch, ev.seq, ev.e);
        // A conversation's replay ends with a snapshot "after" it that adds nothing: the page has caught up.
        const replayed = f.ch.startsWith('conv:') ? this.convs.get(f.ch.slice(5)) : undefined;
        if (replayed)
          s.deliver({
            f: 'snapshot',
            ch: f.ch as ConvChannel,
            seq: ring.seq,
            mode: 'after',
            data: { ...replayed, turns: [], entries: [], beforeTurnId: null },
          });
        return;
      }
    }
    if (f.ch === 'machine') {
      s.deliver({
        f: 'snapshot',
        ch: 'machine',
        seq: ring.seq,
        mode: 'replace',
        data: { machine: this.machine },
      });
      return;
    }
    if (f.ch === 'convs') {
      s.deliver({
        f: 'snapshot',
        ch: 'convs',
        seq: ring.seq,
        mode: 'replace',
        data: {
          conversations: this.conversations,
          projects: [],
          defaultCwd: '/home/fake',
          ...(this.drafts.length > 0 ? { drafts: this.drafts } : {}),
        },
      });
      return;
    }
    const data = this.convs.get(f.ch.slice(5))!;
    if (f.afterEntryId && data.entries.some((e) => e.id === f.afterEntryId)) {
      const at = data.entries.findIndex((e) => e.id === f.afterEntryId);
      s.deliver({
        f: 'snapshot',
        ch: f.ch as ConvChannel,
        seq: ring.seq,
        mode: 'after',
        data: { ...data, entries: data.entries.slice(at + 1), turns: data.turns.slice(-1) },
      });
      return;
    }
    s.deliver({ f: 'snapshot', ch: f.ch as ConvChannel, seq: ring.seq, mode: 'replace', data });
  }

  private deliverEv(s: FakeSocket, ch: Channel, seq: number, e: Event): void {
    s.deliver({ f: 'ev', ch, seq, e } as ServerFrame);
  }

  ring(ch: Channel): Ring {
    let r = this.rings.get(ch);
    if (!r) this.rings.set(ch, (r = { seq: 0, events: [] }));
    return r;
  }

  /** An event on a channel: the next seq, into the ring, to every subscribed socket. Applies it to the fake's state. */
  emit(ch: Channel, e: Event): number {
    const r = this.ring(ch);
    r.seq += 1;
    r.events.push({ seq: r.seq, e });
    while (r.events.length > this.ringSize) r.events.shift();
    this.track(ch, e);
    for (const s of this.sockets) if (s.subs.has(ch)) this.deliverEv(s, ch, r.seq, e);
    return r.seq;
  }

  /** Skips seqs on a channel without telling anyone (a lost frame). */
  skip(ch: Channel, n = 1): void {
    const r = this.ring(ch);
    r.seq += n;
  }

  /** Keeps the fake's snapshots in step with what it emitted. */
  private track(ch: Channel, e: Event): void {
    if (ch === 'convs' && e.type === 'conversation_upsert') {
      this.conversations = [...this.conversations.filter((c) => c.id !== e.conv.id), e.conv];
      return;
    }
    if (!ch.startsWith('conv:')) return;
    const data = this.convs.get(ch.slice(5));
    if (!data) return;
    if (e.type === 'step') data.entries.push(e.entry);
    if (e.type === 'turn_started') data.turns.push(e.turn);
    if (e.type === 'message_upsert')
      data.messages = [...data.messages.filter((m) => m.clientId !== e.message.clientId), e.message];
    if (e.type === 'prompt_state') {
      data.messages = data.messages.map((m) =>
        m.clientId === e.clientId ? { ...m, state: e.state, rev: e.rev } : m,
      );
    }
    if (e.type === 'gate_updated') {
      data.questions = data.questions.map((q) => (q.id === e.id ? { ...q, state: e.state } : q));
      data.permissions = data.permissions.map((p) => (p.id === e.id ? { ...p, state: e.state } : p));
    }
    if (e.type === 'question_asked') data.questions = [...data.questions, e.question];
    if (e.type === 'permission_requested' || e.type === 'plan_proposed')
      data.permissions = [...data.permissions.filter((p) => p.id !== e.permission.id), e.permission];
    if (e.type === 'queue_changed') data.queue = e.queue;
    if (e.type === 'now') {
      const { type: _type, ...now } = e;
      data.now = now;
    }
    if (e.type === 'turn_finished' || e.type === 'turn_failed') data.now = null;
    if (e.type === 'background_upsert')
      data.background = [...data.background.filter((t) => t.id !== e.task.id), e.task];
    if (e.type === 'background_removed') data.background = data.background.filter((t) => t.id !== e.id);
    if (e.type === 'shots_set') data.shots = e.shots;
    if (e.type === 'pictures_set') data.pictures = e.pictures;
  }

  /** Every socket stops hearing the runtime (no close). */
  silence(): void {
    for (const s of this.sockets) s.silent = true;
  }

  /** The runtime closes every socket. */
  dropAll(): void {
    for (const s of [...this.sockets]) s.serverClose();
  }

  /** A restart: a new epoch, seqs from 0, rings empty, sockets closed. */
  restart(epoch = `ep-${Math.random().toString(36).slice(2, 10)}`): void {
    this.epoch = epoch;
    this.rings.clear();
    this.dropAll();
  }

  lastEntryId(conv: string): EntryId | null {
    const es = this.convs.get(conv)?.entries.filter((e) => !e.id.startsWith('live:')) ?? [];
    return es.length ? es[es.length - 1].id : null;
  }

  // ---- HTTP

  fetch = async (url: string, init: RequestInit): Promise<Response> => {
    await new Promise((resolve) => setTimeout(resolve, this.latencyMs));
    const u = new URL(url, 'http://fake');
    const path = u.pathname.replace(/^.*\/api\/v2/, '');
    if (path === '/hello' && this.hello === 'network') throw new TypeError('Failed to fetch');
    if (path === '/hello' && this.hello === 'older') return new Response('<!doctype html>', { status: 200 });
    if (path === '/hello')
      return json({
        proto: 2,
        version: this.welcome.machine.runtime?.version ?? '1',
        features: this.welcome.features,
      });
    if (path === '/machines') return json([this.machine]);
    if (path === '/me') return json({ r: 'refused', code: 'not-found' }, 404);
    if (path.startsWith('/cmd/')) {
      if (this.hang) {
        return new Promise<Response>((_, reject) => {
          init.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
        });
      }
      if (this.gatewayErrors > 0) {
        this.gatewayErrors -= 1;
        return new Response('<html><h1>502 Bad Gateway</h1></html>', { status: 502 });
      }
      const op = path.slice(5);
      const cmd = parseCommand(op, JSON.parse(String(init.body)));
      this.commands.push(cmd);
      const memo = this.ops.get(cmd.body.opId);
      const body = JSON.stringify(cmd.body);
      if (memo) return json(memo.body === body ? memo.result : { r: 'refused', code: 'conflict' });
      const handler = this.handlers.get(cmd.op);
      const result: CmdResult = handler ? await handler(cmd, this) : this.touchConversation();
      // Like the runtime: «not there now» and «no time» are not the command's answer, the same opId may try again.
      if (!(result.r === 'refused' && RETRYABLE_REFUSE_CODES.some((c) => c === result.code)))
        this.ops.set(cmd.body.opId, { body, result });
      if (this.loseAnswers > 0) {
        this.loseAnswers -= 1;
        throw new TypeError('Failed to fetch');
      }
      return json(result);
    }
    return json({ r: 'refused', code: 'not-found' }, 404);
  };

  /** What an op without its own handler does: the first conversation's record again, one rev up. */
  touchConversation(): CmdResult {
    const c = this.conversations[0];
    const seq = this.emit('convs', { type: 'conversation_upsert', conv: { ...c, rev: c.rev + 1 } });
    return { r: 'accepted', ch: 'convs', seq, rev: c.rev + 1 };
  }

  defaultSend(cmd: Extract<Command, { op: 'send' }>): CmdResult {
    const b = cmd.body;
    const ch: ConvChannel = `conv:${b.conversationId}`;
    const seq = this.emit(ch, {
      type: 'message_upsert',
      message: {
        clientId: b.clientId,
        conversationId: b.conversationId,
        text: b.text,
        attachments: [],
        mode: b.mode,
        kind: b.kind,
        createdAt: 1792656000000,
        attempts: 0,
        state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null },
        rev: 1,
      },
    });
    return { r: 'queued', ch, seq };
  }

  defaultCreate(cmd: Extract<Command, { op: 'conversation.create' }>): CmdResult {
    const id = `tab-N3wConv${String(this.nextConv++).padStart(2, '0')}` as const;
    const base = this.conversations[0];
    const conv: Conversation = {
      ...base,
      id,
      title: '',
      titleSource: 'first-message',
      state: {
        k: 'starting',
        since: 1792656000000,
        step: 'machine',
        wait: { slowAt: 1792656015000, deadlineAt: 1792656150000 },
      },
      lastTurn: null,
      queue: { count: cmd.body.first ? 1 : 0, paused: null },
      gate: null,
      rev: 1,
    };
    this.convs.set(id, {
      turns: [],
      entries: [],
      beforeTurnId: null,
      queue: { conversationId: id, items: [], paused: null, rev: 1 },
      messages: [],
      questions: [],
      permissions: [],
      background: [],
      now: null,
    });
    const seq = this.emit('convs', { type: 'conversation_upsert', conv });
    const first = cmd.body.first;
    if (first) {
      // The new channel's ring starts with the first message: a page that subscribes with afterSeq 0 gets it.
      this.emit(`conv:${id}`, {
        type: 'message_upsert',
        message: {
          clientId: first.clientId,
          conversationId: id,
          text: first.text,
          attachments: [],
          mode: 'queue',
          kind: 'prompt',
          createdAt: 1792656000000,
          attempts: 0,
          state: {
            k: 'queued',
            position: 1,
            reason: 'launching',
            wait: { slowAt: 1792656015000, deadlineAt: 1792656900000 },
          },
          rev: 1,
        },
      });
    }
    return { r: 'accepted', ch: 'convs', seq, id };
  }
}

const json = (v: unknown, status = 200) =>
  new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } });
