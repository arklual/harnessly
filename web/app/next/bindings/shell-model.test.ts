import { afterEach, describe, expect, it } from 'vitest';
import { alertOf, cardOf, choiceOf, terminalRuns, terminalStopText } from '@/next/bindings/shell-model';
import { applyMachineEvent } from '@/next/core/realtime/apply';
import { pinLocale } from '@/next/i18n';
import { MACHINE_HEALTH, MACHINE_TONE } from '@/next/features/sidebar/types';
import { machinesStore, putMachines, type MachineView } from '@/next/stores/machines';
import type { HealthIssue, Machine, MachineHealth } from '@/next/protocol/v2';

const view = (reach: Machine['reach'], runtime: MachineView['runtime']): MachineView => ({
  machine: {
    id: 'dev-department',
    name: 'acme-dev-department',
    host: 'acme-dev-department',
    reach,
    lastSeenAt: null,
    runtime: null,
    agents: [],
  },
  runtime,
  epoch: null,
  features: [],
  limits: null,
});

describe('the other machines of the card', () => {
  it('a machine that is fine says nothing under its name, not its host', () => {
    expect(choiceOf(view('online', 'v2'))).toMatchObject({ off: false, note: '' });
  });

  it('a machine that does not answer or runs an old runtime says so at once, and cannot be chosen', () => {
    const away = choiceOf(view('unreachable', 'unknown'));
    const old = choiceOf(view('online', 'outdated'));
    expect(away.off).toBe(true);
    expect(away.note).not.toBe('');
    expect(old.off).toBe(true);
    expect(old.note).not.toBe('');
    expect(old.note).not.toBe(away.note);
  });
});

const GB = 1024 ** 3;
const health = (...issues: HealthIssue[]): MachineHealth => ({
  level: issues[0]?.level ?? 'ok',
  since: 1792656000000,
  issues,
});
const loaded = (h: MachineHealth, reach: Machine['reach'] = 'online'): MachineView => {
  const v = view(reach, 'v2');
  return {
    ...v,
    machine: {
      ...v.machine,
      health: h,
      load: {
        at: 1792656000000,
        cpu: 0.4,
        memUsed: 20 * GB,
        memTotal: 32 * GB,
        diskFree: 2.6 * GB,
        diskTotal: 260 * GB,
        busyAgents: 0,
      },
    },
  };
};

describe("the machine's health", () => {
  afterEach(() => pinLocale(null));

  it('the dot, the lines and their reasons follow what the runtime judged, not the page', () => {
    const card = cardOf(
      loaded(health({ metric: 'disk', level: 'critical' }, { metric: 'mem', level: 'warn' })),
      false,
    );
    expect(card.health).toBe(MACHINE_HEALTH.critical);
    const byId = Object.fromEntries((card.meters ?? []).map((m) => [m.id, m]));
    expect(byId.disk).toMatchObject({ tone: MACHINE_TONE.critical });
    expect(byId.disk?.reason).toBeTruthy();
    expect(byId.mem).toMatchObject({ tone: MACHINE_TONE.warn });
    expect(byId.cpu).toMatchObject({ tone: MACHINE_TONE.normal, reason: null });
    // 2.6 GB free of 260 is under any line the page could draw, yet the runtime said ok: the page says ok.
    expect(cardOf(loaded(health()), false)).toMatchObject({ health: MACHINE_HEALTH.ok, alert: null });
  });

  it('only a critical machine that answers has a banner, with the figure that ran out', () => {
    pinLocale('ru');
    const critical = loaded(health({ metric: 'disk', level: 'critical' }));
    expect(alertOf(critical.machine, false)?.text).toBe('Диск почти заполнен — 2,6 ГБ свободно');
    expect(alertOf(critical.machine, true)).toBeNull();
    expect(alertOf(loaded(health({ metric: 'disk', level: 'warn' })).machine, false)).toBeNull();
    expect(
      alertOf(loaded(health({ metric: 'disk', level: 'critical' }), 'unreachable').machine, false),
    ).toBeNull();
    // Worse is another key: a banner the person hid comes back.
    const worse = loaded(health({ metric: 'disk', level: 'critical' }, { metric: 'mem', level: 'critical' }));
    expect(alertOf(worse.machine, false)?.key).not.toBe(alertOf(critical.machine, false)?.key);
  });

  it('another machine shows its level on its dot', () => {
    expect(choiceOf(loaded(health({ metric: 'mem', level: 'warn' }))).health).toBe(MACHINE_HEALTH.warn);
    expect(choiceOf(loaded(health({ metric: 'mem', level: 'critical' }))).health).toBe(
      MACHINE_HEALTH.critical,
    );
  });

  it('a machine_status without health keeps the health the page holds', () => {
    const m = loaded(health({ metric: 'disk', level: 'critical' })).machine;
    putMachines([m]);
    applyMachineEvent(m.id, { type: 'machine_status', reach: 'online', runtime: null, agents: [] });
    expect(machinesStore.get(m.id)?.machine.health?.level).toBe('critical');
    applyMachineEvent(m.id, {
      type: 'machine_status',
      reach: 'online',
      runtime: null,
      agents: [],
      health: health(),
    });
    expect(machinesStore.get(m.id)?.machine.health?.level).toBe('ok');
  });
});

describe("a chat's terminal", () => {
  afterEach(() => pinLocale(null));

  it('runs while the agent is up, and not once it was ended or is gone', () => {
    expect(terminalRuns({ state: { k: 'idle', since: 1 } })).toBe(true);
    expect(terminalRuns({ state: { k: 'gone', since: 1, reason: 'tmux-lost' } })).toBe(false);
    expect(
      terminalRuns({
        state: { k: 'stopped', since: 1, resumable: true },
        terminalStop: { at: 1, by: 'person' },
      }),
    ).toBe(false);
  });

  it('a stopped chat says why under its last turn, in whole hours for the idle stop', () => {
    pinLocale('ru');
    const stopped = { k: 'stopped', since: 1, resumable: true } as const;
    expect(
      terminalStopText({ state: stopped, terminalStop: { at: 1, by: 'idle', idleMs: 2 * 3_600_000 + 9 } }),
    ).toBe('Терминал остановлен: 2 ч без работы');
    expect(terminalStopText({ state: stopped, terminalStop: { at: 1, by: 'person' } })).toBe(
      'Терминал остановлен',
    );
    pinLocale('en');
    expect(terminalStopText({ state: stopped, terminalStop: { at: 1, by: 'idle', idleMs: 3_600_000 } })).toBe(
      'Terminal stopped: 1 h without work',
    );
  });

  it('says nothing once the agent is back, or when nobody ended it', () => {
    expect(
      terminalStopText({ state: { k: 'idle', since: 1 }, terminalStop: { at: 1, by: 'idle', idleMs: 1 } }),
    ).toBe(null);
    expect(terminalStopText({ state: { k: 'stopped', since: 1, resumable: true } })).toBe(null);
  });
});
