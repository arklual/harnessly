import { describe, expect, it } from 'vitest';
import { itemsOfCodes, itemsOfMachines, machineDetail, orderItems } from '@/next/bindings/smart-notice';
import { SMART_KIND, type ISmartItem } from '@/next/features/smart-notice/smart-notice';
import type { MachineProc, MachineStats } from '@/next/protocol/v2';

const item = (
  key: string,
  kind: ISmartItem['kind'],
  at: number,
  until: number | null = null,
): ISmartItem => ({
  key,
  kind,
  chatId: null,
  icon: 'circle',
  title: key,
  text: '',
  at,
  until,
});

const proc = (
  pid: number,
  kind: MachineProc['kind'],
  cpu: number,
  rss: number,
  guard = false,
): MachineProc => ({
  pid,
  startTime: `${pid}`,
  name: `p${pid}`,
  command: `p${pid}`,
  cpu,
  rss,
  kind,
  port: null,
  stuck: false,
  guard,
});

const stats = (procs: MachineProc[]): MachineStats => ({
  at: 1,
  cpu: 0.97,
  cores: [],
  mem: { used: 9, total: 10 },
  net: { rx: 0, tx: 0 },
  disk: { path: '/', total: 100, free: 5, parts: [], measuredAt: null, partial: false },
  history: [],
  procs,
});

describe('orderItems', () => {
  it('puts a code first, then a permission, the machine and the questions, then what broke, then what is done', () => {
    const order = orderItems([
      item('done', SMART_KIND.done, 5),
      item('q', SMART_KIND.question, 3),
      item('failed', SMART_KIND.failed, 4),
      item('machine', SMART_KIND.machine, 2),
      item('perm', SMART_KIND.permission, 6),
      item('code', SMART_KIND.code, 7, 100),
    ]).map((i) => i.key);
    expect(order).toEqual(['code', 'perm', 'machine', 'q', 'failed', 'done']);
  });

  it('keeps the codes by their deadline and what is done newest first', () => {
    const order = orderItems([
      item('c2', SMART_KIND.code, 1, 200),
      item('c1', SMART_KIND.code, 2, 100),
      item('d1', SMART_KIND.done, 1),
      item('d2', SMART_KIND.done, 2),
    ]).map((i) => i.key);
    expect(order).toEqual(['c1', 'c2', 'd2', 'd1']);
  });
});

describe('itemsOfCodes', () => {
  it('moves a code to this page clock', () => {
    const [code] = itemsOfCodes(
      [{ id: 'a', conv: '', title: 'npm', why: '', digits: 6, at: 1000, until: 61000 }],
      500,
    );
    expect(code).toMatchObject({ key: 'code:a', kind: SMART_KIND.code, chatId: null, at: 500, until: 60500 });
  });
});

describe('itemsOfMachines', () => {
  it('makes a machine in the red an item of its own', () => {
    const [m] = itemsOfMachines([
      {
        id: 'vm',
        name: 'Моя ВМ',
        since: 7,
        alert: { key: 'vm:mem', icon: 'memory-stick', text: '', metric: 'mem' },
      },
    ]);
    expect(m).toMatchObject({
      key: 'machine:vm:mem',
      kind: SMART_KIND.machine,
      title: 'Моя ВМ',
      at: 7,
      chatId: null,
    });
  });
});

describe('machineDetail', () => {
  it('says what runs short before the figures come', () => {
    expect(machineDetail('mem', null)).toMatchObject({ k: 'machine', level: 1, heavy: [] });
  });

  it('lists the agents together and the heaviest programs, and lets stop only what may be stopped', () => {
    const detail = machineDetail(
      'mem',
      stats([
        proc(1, 'agent', 0, 300),
        proc(2, 'agent', 0, 300),
        proc(3, 'dev', 0, 500),
        proc(4, 'service', 0, 400),
        proc(5, 'other', 0, 10),
        proc(6, 'other', 0, 50, true),
      ]),
    );
    if (detail.k !== 'machine') throw new Error(detail.k);
    expect(detail.level).toBeCloseTo(0.9);
    expect(detail.heavy.map((h) => [h.key, h.stoppable])).toEqual([
      ['agents', false],
      ['3:3', true],
      ['4:4', false],
    ]);
  });

  it('weighs by the processor when that runs short, and lists no programs for a full disk', () => {
    const cpu = machineDetail('cpu', stats([proc(1, 'other', 10, 900), proc(2, 'dev', 90, 1)]));
    if (cpu.k !== 'machine') throw new Error(cpu.k);
    expect(cpu.heavy[0]?.key).toBe('2:2');
    const disk = machineDetail('disk', stats([proc(1, 'dev', 90, 900)]));
    if (disk.k !== 'machine') throw new Error(disk.k);
    expect(disk.heavy).toEqual([]);
    expect(disk.level).toBeCloseTo(0.95);
  });
});
