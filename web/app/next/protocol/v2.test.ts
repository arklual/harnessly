import { afterEach, describe, expect, it } from 'vitest';
import * as v2 from './v2';
import {
  parseClientFrame,
  parseCmdResult,
  parseCommand,
  parseMachineRefusal,
  parsePage,
  parseRefusal,
  parseServerFrame,
  ProtocolError,
  setLenientDecoding,
  CLIENT_FRAME_KINDS,
} from './v2';

// The fixtures and the schema are the ones go test reads in internal/wire.
const fixtures = import.meta.glob<unknown>('../../../../internal/wire/testdata/*.json', {
  eager: true,
  import: 'default',
});
const [schema] = Object.values(
  import.meta.glob<{ $defs: Record<string, { enum?: string[]; oneOf?: { properties?: object }[] }> }>(
    '../../../../internal/wire/schema/frames.json',
    { eager: true, import: 'default' },
  ),
);

const name = (path: string) =>
  path
    .split('/')
    .pop()!
    .replace(/\.json$/, '');

/** Reads a fixture the way its side of the wire would. */
function parseFixture(doc: unknown): unknown {
  const o = doc as Record<string, unknown>;
  if (typeof o.f === 'string') {
    return (CLIENT_FRAME_KINDS as readonly string[]).includes(o.f)
      ? parseClientFrame(o)
      : parseServerFrame(o);
  }
  if (typeof o.op === 'string') {
    const cmd = parseCommand(o.op, o.body);
    return { op: cmd.op, body: cmd.body, result: parseCmdResult(o.result) };
  }
  const http = o.http as keyof typeof parsePage;
  if (!Object.hasOwn(parsePage, http)) throw new Error(`unknown fixture kind ${JSON.stringify(http)}`);
  const response = o.response as Record<string, unknown>;
  return { http, response: 'r' in response ? parseRefusal(response) : parsePage[http](response) };
}

describe('v2 fixtures', () => {
  const entries = Object.entries(fixtures);

  it('are at least 30', () => {
    expect(entries.length).toBeGreaterThanOrEqual(30);
  });

  it.each(entries.map(([path, doc]) => [name(path), doc]))('%s parses without loss', (_, doc) => {
    expect(parseFixture(doc)).toStrictEqual(doc);
  });
});

describe('v2 closed lists', () => {
  // Every closed list of the schema and the TS array that mirrors it; the decoder tables are tied to the arrays by
  // `satisfies Record<…>` in v2.ts, so a kind missing from either side fails typecheck.
  const lists: Record<string, readonly string[]> = {
    Provider: v2.PROVIDERS,
    Feature: v2.FEATURES,
    Reach: v2.REACHES,
    HealthLevel: v2.HEALTH_LEVELS,
    HealthMetric: v2.HEALTH_METRICS,
    AgentInstallStatus: v2.AGENT_INSTALL_STATUSES,
    Effort: v2.EFFORTS,
    TitleSource: v2.TITLE_SOURCES,
    AgentStateKind: v2.AGENT_STATE_KINDS,
    LaunchStep: v2.LAUNCH_STEPS,
    LaunchFailure: v2.LAUNCH_FAILURES,
    GoneReason: v2.GONE_REASONS,
    TurnStatus: v2.TURN_STATUSES,
    TurnFailureCode: v2.TURN_FAILURE_CODES,
    FileOp: v2.FILE_OPS,
    OutputKind: v2.OUTPUT_KINDS,
    OutputOp: v2.OUTPUT_OPS,
    EntryKind: v2.ENTRY_KINDS,
    SubagentStatus: v2.SUBAGENT_STATUSES,
    NoticeLevel: v2.NOTICE_LEVELS,
    PeerNoteKind: v2.PEER_NOTE_KINDS,
    AttachmentKind: v2.ATTACHMENT_KINDS,
    RefKind: v2.REF_KINDS,
    MessageMode: v2.MESSAGE_MODES,
    MessageKind: v2.MESSAGE_KINDS,
    MessageStateKind: v2.MESSAGE_STATE_KINDS,
    QueueReason: v2.QUEUE_REASONS,
    MessageFailure: v2.MESSAGE_FAILURES,
    PauseReason: v2.PAUSE_REASONS,
    GateKind: v2.GATE_KINDS,
    PermissionKind: v2.PERMISSION_KINDS,
    PermissionScope: v2.PERMISSION_SCOPES,
    GateStateKind: v2.GATE_STATE_KINDS,
    GoalStatus: v2.GOAL_STATUSES,
    GateCloseReason: v2.GATE_CLOSE_REASONS,
    GateRefusal: v2.GATE_REFUSALS,
    BackgroundKind: v2.BACKGROUND_KINDS,
    BackgroundStatus: v2.BACKGROUND_STATUSES,
    SnapshotMode: v2.SNAPSHOT_MODES,
    FrameErrorCode: v2.FRAME_ERROR_CODES,
    ClientFrameKind: v2.CLIENT_FRAME_KINDS,
    ServerFrameKind: v2.SERVER_FRAME_KINDS,
    MachineEventType: v2.MACHINE_EVENT_TYPES,
    ConvsEventType: v2.CONVS_EVENT_TYPES,
    ConvEventType: v2.CONV_EVENT_TYPES,
    CompactionPhase: v2.COMPACTION_PHASES,
    Op: v2.OPS,
    PromoteTo: v2.PROMOTE_TOS,
    ResultKind: v2.RESULT_KINDS,
    RefuseCode: v2.REFUSE_CODES,
    RetryableRefuseCode: v2.RETRYABLE_REFUSE_CODES,
    DesignKind: v2.DESIGN_KINDS,
    DesignRole: v2.DESIGN_ROLES,
    FileKind: v2.FILE_KINDS,
    AppID: v2.APP_IDS,
    OpenTab: v2.OPEN_TABS,
    AppState: v2.APP_STATES,
    AppReason: v2.APP_REASONS,
    AppSectionID: v2.APP_SECTION_IDS,
    AppFlag: v2.APP_FLAGS,
    AppTone: v2.APP_TONES,
    AppRole: v2.APP_ROLES,
    AppPipeline: v2.APP_PIPELINES,
    AgentOptionKey: v2.AGENT_OPTION_KEYS,
    AgentMcpKind: v2.AGENT_MCP_KINDS,
    AgentAccountMethod: v2.AGENT_ACCOUNT_METHODS,
    DiskPartID: v2.DISK_PART_IDS,
    MachineProcKind: v2.MACHINE_PROC_KINDS,
    MachineStopReason: v2.MACHINE_STOP_REASONS,
    TaskStatus: v2.TASK_STATUSES,
    TerminalStopBy: v2.TERMINAL_STOP_BYS,
    IdleStop: v2.IDLE_STOPS,
    FileChange: v2.FILE_CHANGES,
    OriginKind: v2.ORIGIN_KINDS,
    OriginVia: v2.ORIGIN_VIAS,
    HandoffMode: v2.HANDOFF_MODES,
    DraftMode: v2.DRAFT_MODES,
    OnOff: v2.ON_OFFS,
    LimitWindow: v2.LIMIT_WINDOWS,
    AutoContinueWhy: v2.AUTO_CONTINUE_WHYS,
    LimitAction: v2.LIMIT_ACTIONS,
    ProfileVendor: v2.PROFILE_VENDORS,
    ProfileTestReason: v2.PROFILE_TEST_REASONS,
    MessageStatusKind: v2.MESSAGE_STATUS_KINDS,
    HandedWhere: v2.HANDED_WHERES,
    HttpKind: Object.keys(parsePage),
  };

  it('cover every list of the schema', () => {
    const schemaLists = Object.entries(schema.$defs)
      .filter(([, def]) => def.enum)
      .map(([n]) => n)
      .sort();
    expect(Object.keys(lists).sort()).toEqual(schemaLists);
  });

  it.each(Object.entries(lists))('%s is the schema list', (list, values) => {
    expect([...values]).toEqual(schema.$defs[list].enum);
  });
});

describe('v2 parser', () => {
  const welcome = Object.entries(fixtures).find(([p]) => name(p) === 'frame.server.welcome')![1];
  const refused = (frame: unknown) => expect(() => parseServerFrame(frame)).toThrow(ProtocolError);

  it('refuses an unknown frame', () => refused({ f: 'evt', t: 1 }));
  it('refuses a page frame from the runtime', () => refused({ f: 'ping', t: 1 }));
  it('refuses an unknown event', () =>
    refused({ f: 'ev', ch: 'convs', seq: 1, e: { type: 'conversation_moved', id: 'tab-a' } }));
  it('refuses an event on the wrong channel', () =>
    refused({ f: 'ev', ch: 'machine', seq: 1, e: { type: 'conversation_removed', id: 'tab-a' } }));
  it('refuses an unknown field', () => refused({ ...(welcome as object), extra: 1 }));
  it('refuses a missing field', () => {
    const { limits: _, ...rest } = welcome as Record<string, unknown>;
    refused(rest);
  });
  it('refuses a field of another kind', () =>
    refused({
      f: 'ev',
      ch: 'convs',
      seq: 1,
      e: { type: 'conversation_state', id: 'tab-a', rev: 1, state: { k: 'ready', since: 1, turnId: null } },
    }));
  it('refuses a state that lives only on the page', () =>
    refused({
      f: 'ev',
      ch: 'conv:tab-a',
      seq: 1,
      e: { type: 'prompt_state', clientId: 'c', rev: 1, state: { k: 'submitting' } },
    }));
  it('refuses a channel that is not a conversation', () =>
    refused({ f: 'error', ch: 'conv:pane-1', code: 'proto' }));
  it('refuses seq 0 on an event', () =>
    refused({ f: 'ev', ch: 'convs', seq: 0, e: { type: 'conversation_removed', id: 'tab-a' } }));
  it('refuses an unknown refuse code', () =>
    expect(() => parseCmdResult({ r: 'refused', code: 'busy' })).toThrow(ProtocolError));
  it('refuses an unknown op', () =>
    expect(() => parseCommand('send-now', { opId: 'a' })).toThrow(ProtocolError));
  it('refuses a send whose opId is not its clientId', () =>
    expect(() =>
      parseCommand('send', {
        opId: 'a',
        clientId: 'b',
        conversationId: 'tab-a',
        text: '',
        attachmentIds: [],
        mode: 'queue',
        kind: 'prompt',
      }),
    ).toThrow(ProtocolError));
  it('refuses afterSeq without an epoch', () =>
    expect(() => parseClientFrame({ f: 'sub', ch: 'convs', afterSeq: 3 })).toThrow(ProtocolError));
  it('refuses a live turn that ended', () => {
    const started = Object.entries(fixtures).find(([p]) => name(p) === 'ev.turn_started')![1] as {
      e: { turn: object };
    };
    refused({ ...started, e: { ...started.e, turn: { ...started.e.turn, endedAt: 1 } } });
  });
  it('refuses a waiting state without its term', () =>
    refused({
      f: 'ev',
      ch: 'conv:tab-a',
      seq: 1,
      e: { type: 'prompt_state', clientId: 'c', rev: 1, state: { k: 'in-input', wait: null } },
    }));
  it('refuses a random entry id', () =>
    refused({
      f: 'ev',
      ch: 'conv:tab-a',
      seq: 1,
      e: {
        type: 'entry_replace',
        oldId: '9f2c',
        entry: { t: 'interrupt', id: 'x:1#0', turnId: null, at: 1, rev: 1 },
      },
    }));
  it('refuses a live entry as a turn', () =>
    refused({
      f: 'ev',
      ch: 'convs',
      seq: 1,
      e: {
        type: 'conversation_state',
        id: 'tab-a',
        rev: 1,
        state: { k: 'working', since: 1, turnId: 'live:x:1#0', compacting: false },
      },
    }));
  it('tells a wait that is slow from one that is overdue', () => {
    const w = { slowAt: 3000, deadlineAt: 12000 };
    expect([0, 3000, 11999, 12000].map((at) => v2.waitPhase(w, at))).toEqual([
      'normal',
      'slow',
      'slow',
      'overdue',
    ]);
  });
  const answer = (answers: unknown) => () =>
    parseCommand('answer', { opId: 'a', conversationId: 'tab-a', questionId: 'q:1', answers });
  it('refuses an answer with both choices and text', () =>
    expect(answer([{ i: 0, choices: [1], text: 'x' }])).toThrow(ProtocolError));
  it('refuses an answer with neither', () => expect(answer([{ i: 0, choices: [] }])).toThrow(ProtocolError));
  it('takes text instead of choices', () => expect(answer([{ i: 0, choices: [], text: 'x' }])).not.toThrow());
  it('refuses a permission id for a question', () =>
    expect(() =>
      parseCommand('answer', { opId: 'a', conversationId: 'tab-a', questionId: 'p:1', chatAbout: true }),
    ).toThrow(ProtocolError));
  it('names where it failed', () => {
    try {
      parseServerFrame({ f: 'pong', t: -1 });
    } catch (e) {
      expect(e).toBeInstanceOf(ProtocolError);
      expect((e as ProtocolError).path).toBe('$.t');
      return;
    }
    throw new Error('parsed');
  });
});

describe('v2 parser in a production build', () => {
  const welcome = Object.entries(fixtures).find(([p]) => name(p) === 'frame.server.welcome')![1] as Record<
    string,
    unknown
  >;
  afterEach(() => setLenientDecoding(false));

  it('drops a field it has no name for', () => {
    setLenientDecoding(true);
    const frame = parseServerFrame({ ...welcome, fromTheFuture: 1 }) as unknown as Record<string, unknown>;
    expect('fromTheFuture' in frame).toBe(false);
    expect(frame.f).toBe('welcome');
  });
  it('drops an unknown field inside a nested object', () => {
    setLenientDecoding(true);
    const limits = { ...(welcome.limits as object), fromTheFuture: 1 };
    const frame = parseServerFrame({ ...welcome, limits }) as unknown as { limits: Record<string, unknown> };
    expect('fromTheFuture' in frame.limits).toBe(false);
  });
  it('drops an optional field whose value it does not know', () => {
    const upsert = Object.entries(fixtures).find(
      ([p]) => name(p) === 'ev.conversation_upsert-ultracode',
    )![1] as {
      e: { conv: { lastTurn: Record<string, unknown> } };
    };
    const moved = structuredClone(upsert);
    moved.e.conv.lastTurn.failure = 'from-the-future';
    expect(() => parseServerFrame(moved)).toThrow(ProtocolError);
    setLenientDecoding(true);
    const frame = parseServerFrame(moved) as unknown as typeof upsert;
    expect('failure' in frame.e.conv.lastTurn).toBe(false);
    expect(frame.e.conv.lastTurn.status).toBe('done');
  });
  it('still refuses a missing field', () => {
    setLenientDecoding(true);
    const { limits: _, ...rest } = welcome;
    expect(() => parseServerFrame(rest)).toThrow(ProtocolError);
  });
  it('still refuses an unknown frame or event', () => {
    setLenientDecoding(true);
    expect(() => parseServerFrame({ f: 'evt', t: 1 })).toThrow(ProtocolError);
  });
  it('still refuses an unknown value in a required field', () => {
    setLenientDecoding(true);
    expect(() => parseServerFrame({ f: 'error', code: 'from-the-future' })).toThrow(ProtocolError);
  });
  it('is strict again when switched off', () => {
    setLenientDecoding(false);
    expect(() => parseServerFrame({ ...welcome, fromTheFuture: 1 })).toThrow(ProtocolError);
  });
});

describe('v2 machine tool pages', () => {
  const stats = Object.entries(fixtures).find(([p]) => name(p) === 'http.machineStats')![1] as {
    response: Record<string, unknown>;
  };
  it('refuse a share above 1, a pid below 2 and a start time that is not digits', () => {
    expect(() => parsePage.machineStats({ ...stats.response, cpu: 1.2 })).toThrow(ProtocolError);
    const proc = (stats.response.procs as Record<string, unknown>[])[0];
    expect(() => parsePage.machineStats({ ...stats.response, procs: [{ ...proc, pid: 1 }] })).toThrow(
      ProtocolError,
    );
    expect(() =>
      parsePage.machineStats({ ...stats.response, procs: [{ ...proc, startTime: '12a' }] }),
    ).toThrow(ProtocolError);
    expect(() => parsePage.machineStats({ ...stats.response, procs: [{ ...proc, port: 70000 }] })).toThrow(
      ProtocolError,
    );
  });
  it('read a stop that went through and one that was refused with its reason', () => {
    expect(parsePage.machineStop({ stopped: true, signal: 'SIGKILL' }).signal).toBe('SIGKILL');
    expect(() => parsePage.machineStop({ stopped: false, signal: 'SIGTERM' })).toThrow(ProtocolError);
    expect(
      parseMachineRefusal({ r: 'refused', code: 'forbidden', reason: 'protected', message: 'No.' }).reason,
    ).toBe('protected');
    expect(() =>
      parseMachineRefusal({ r: 'refused', code: 'forbidden', reason: 'because', message: 'No.' }),
    ).toThrow(ProtocolError);
  });
});

describe('v2 apps page', () => {
  const ready = Object.entries(fixtures).find(([p]) => name(p) === 'http.apps')![1] as {
    response: { apps: Record<string, unknown>[] } & Record<string, unknown>;
  };
  const off = Object.entries(fixtures).find(([p]) => name(p) === 'http.appsOff')![1] as {
    response: { apps: Record<string, unknown>[] } & Record<string, unknown>;
  };
  it('reads an app that is ready and one that says why it is not', () => {
    expect(parsePage.apps(ready.response).apps.map((app) => app.state)).toEqual(['ready', 'ready']);
    const apps = parsePage.apps(off.response).apps;
    expect(apps.map((app) => app.reason)).toEqual(['no-token', 'unreachable']);
    expect(apps[0]?.sections).toEqual([]);
  });
  it('refuses a reason that does not go with the state', () => {
    const [tracker] = ready.response.apps;
    expect(() => parsePage.apps({ ...ready.response, apps: [{ ...tracker, reason: 'no-token' }] })).toThrow(
      ProtocolError,
    );
    const [away] = off.response.apps;
    expect(() => parsePage.apps({ ...off.response, apps: [{ ...away, reason: null }] })).toThrow(
      ProtocolError,
    );
    expect(() =>
      parsePage.apps({ ...off.response, apps: [{ ...away, sections: ready.response.apps[0]?.sections }] }),
    ).toThrow(ProtocolError);
  });
  it('refuses a link that is not a web link, a flag it does not know and a stale term below a day', () => {
    const rows = (patch: Record<string, unknown>) => {
      const [tracker] = ready.response.apps as { sections: { rows: Record<string, unknown>[] }[] }[];
      const section = tracker!.sections[0]!;
      return {
        ...ready.response,
        apps: [{ ...tracker, sections: [{ ...section, rows: [{ ...section.rows[0], ...patch }] }] }],
      };
    };
    expect(() => parsePage.apps(rows({ url: 'javascript:alert(1)' }))).toThrow(ProtocolError);
    expect(() => parsePage.apps(rows({ flag: 'merged' }))).toThrow(ProtocolError);
    expect(() => parsePage.apps({ ...ready.response, staleDays: 0 })).toThrow(ProtocolError);
  });
  it('reads an epic with its subtasks and a merge request with what GitLab says of it', () => {
    const [tracker, gitlab] = parsePage.apps(ready.response).apps;
    const epic = tracker!.sections[0]!.rows[0]!;
    expect(epic.children.map((child) => [child.id, child.done])).toEqual([
      ['ABC-13', true],
      ['ABC-14', false],
    ]);
    expect(tracker!.sections[1]!.rows[0]!.parent?.id).toBe('ABC-12');
    expect(tracker!.me).toBe('Ann Lee');
    const mr = gitlab!.sections[0]!.rows[0]!;
    expect(mr.mr).toMatchObject({ ready: true, pipeline: 'success', approvals: { given: 1, required: 2 } });
    expect(mr.me).toEqual(['reviewer']);
  });
  it('refuses a pipeline, a role and a subtask link it does not know', () => {
    const patched = (section: number, patch: (row: Record<string, unknown>) => Record<string, unknown>) => {
      const feeds = ready.response.apps as { sections: { rows: Record<string, unknown>[] }[] }[];
      const app = feeds[section]!;
      const part = app.sections[0]!;
      return {
        ...ready.response,
        apps: ready.response.apps.map((feed) =>
          feed === app ? { ...app, sections: [{ ...part, rows: [patch(part.rows[0]!)] }] } : feed,
        ),
      };
    };
    expect(() =>
      parsePage.apps(patched(1, (row) => ({ ...row, mr: { ...(row.mr as object), pipeline: 'green' } }))),
    ).toThrow(ProtocolError);
    expect(() => parsePage.apps(patched(1, (row) => ({ ...row, me: ['boss'] })))).toThrow(ProtocolError);
    expect(() =>
      parsePage.apps(patched(0, (row) => ({ ...row, children: [{ id: 'ABC-1', title: 't', url: '/x' }] }))),
    ).toThrow(ProtocolError);
  });
});

describe('v2 files pages', () => {
  it('read the refusal with its message', () =>
    expect(v2.parseFileRefusal({ r: 'refused', code: 'unsupported', message: 'Not text.' })).toEqual({
      r: 'refused',
      code: 'unsupported',
      message: 'Not text.',
    }));
  it('refuse a refusal without a message', () =>
    expect(() => v2.parseFileRefusal({ r: 'refused', code: 'forbidden' })).toThrow(ProtocolError));
  it('refuse an entry of a kind they do not know', () =>
    expect(() =>
      parsePage.files({ path: '', entries: [{ name: 'a', path: 'a', kind: 'link' }], truncated: false }),
    ).toThrow(ProtocolError));
});

describe('v2 runtime settings', () => {
  it('read the switches of going on by itself when the runtime has them', () =>
    expect(
      parsePage.runtimeSettings({ idleStop: '2h', limitContinue: 'on', restartContinue: 'off' }),
    ).toEqual({ idleStop: '2h', limitContinue: 'on', restartContinue: 'off' }));
  it('read the settings of an older runtime without them', () =>
    expect(parsePage.runtimeSettings({ idleStop: '1h' })).toEqual({ idleStop: '1h' }));
  it('refuse a switch that is neither on nor off', () =>
    expect(() => parsePage.runtimeSettings({ idleStop: '1h', limitContinue: 'auto' })).toThrow(
      ProtocolError,
    ));
});

describe('backend profiles', () => {
  const fixture = (n: string) => Object.entries(fixtures).find(([p]) => name(p) === n)![1];
  afterEach(() => setLenientDecoding(false));

  it('reads the profiles a machine offers and the profile of a chat', () => {
    const status = parseServerFrame(fixture('ev.machine_status-profiles')) as unknown as {
      e: { agents: v2.MachineAgent[] };
    };
    expect(status.e.agents[0]!.profiles?.map((p) => [p.id, p.configured])).toEqual([
      ['kimi', true],
      ['glm', false],
    ]);
    const upsert = parseServerFrame(fixture('ev.conversation_upsert-profile')) as unknown as {
      e: { conv: { model: v2.Model } };
    };
    expect(upsert.e.conv.model.profile).toBe('kimi');
  });
  it('names the profile beside the model in a command', () => {
    const create = fixture('cmd.conversation.create-profile') as { body: unknown };
    expect(parseCommand('conversation.create', create.body).body).toMatchObject({ profile: 'kimi' });
    const handoff = fixture('cmd.conversation.handoff-profile') as { body: unknown };
    expect(parseCommand('conversation.handoff', handoff.body).body).toMatchObject({ profile: 'xai' });
  });
  it('refuses a profile id and a key tail out of shape', () => {
    const create = structuredClone(fixture('cmd.conversation.create-profile') as { body: object });
    expect(() => parseCommand('conversation.create', { ...create.body, profile: 'Kimi' })).toThrow(
      ProtocolError,
    );
    expect(() => parsePage.profileKey({ configured: true, tail: '…34' })).toThrow(ProtocolError);
    expect(parsePage.profileKey({ configured: false, tail: null })).toEqual({
      configured: false,
      tail: null,
    });
  });
  it('asks for the cap', () => {
    expect(v2.CAPS_QUERY).toBe('caps=delegation,workflows,profiles');
  });
});
