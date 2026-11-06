// What the «Настройка машины» card shows for a view of the setup: the chain of stages (accesses, code, setup scripts),
// what is inside the current one, the line under the chain (what goes on now) and the one thing wanted of the person.
// Pure: the card and its tests take it as it is.
import { t } from '@/next/i18n';
import {
  BASE_STATUS,
  SETUP_KIND,
  SETUP_STATUS,
  type ISetupItem,
  type ISetupView,
  type TSetupKind,
} from '@/next/features/setup/api';

/** How a stage or a step stands, as the board's marks: done, run, you (wanted of the person), fail, wait. */
export const MARK = { done: 'done', run: 'run', you: 'you', fail: 'fail', wait: 'wait' } as const;
export type TMark = (typeof MARK)[keyof typeof MARK];

/** At most this many steps are named inside a stage; the rest are counted. */
export const INSIDE_MAX = 8;

const STAGES: readonly { kind: TSetupKind; key: string }[] = [
  { kind: SETUP_KIND.base, key: 'next.setup.stage.base' },
  { kind: SETUP_KIND.access, key: 'next.setup.stage.access' },
  { kind: SETUP_KIND.repo, key: 'next.setup.stage.code' },
  { kind: SETUP_KIND.claude, key: 'next.setup.stage.claude' },
  { kind: SETUP_KIND.plugin, key: 'next.setup.stage.plugins' },
  { kind: SETUP_KIND.script, key: 'next.setup.stage.scripts' },
];

const RUN_KEY: Record<TSetupKind, string> = {
  base: 'next.setup.run.base',
  access: 'next.setup.run.access',
  repo: 'next.setup.run.repo',
  'claude-settings': 'next.setup.run.claude',
  plugin: 'next.setup.run.plugin',
  script: 'next.setup.run.script',
};

export const markOf = (it: ISetupItem): TMark => {
  if (it.status === SETUP_STATUS.ok) return MARK.done;
  if (it.status === SETUP_STATUS.error) return MARK.fail;
  if (it.status === SETUP_STATUS.running) return MARK.run;
  // A tool the person installs by hand is wanted of them.
  if (it.base?.command) return MARK.you;
  return it.secret ? MARK.you : MARK.wait;
};

const stageMark = (marks: readonly TMark[]): TMark => {
  if (marks.includes(MARK.fail)) return MARK.fail;
  if (marks.includes(MARK.you)) return MARK.you;
  if (marks.includes(MARK.run)) return MARK.run;
  if (marks.every((m) => m === MARK.done)) return MARK.done;
  return MARK.wait;
};

export interface ICardStage {
  name: string;
  mark: TMark;
  /** «3/36» on the current stage. */
  count: string;
  /** The link before the stage: done when the stage before it is. */
  linkDone: boolean;
}

export interface ICardStep {
  name: string;
  mark: TMark;
}

export const ACTION = {
  retry: 'retry',
  safe: 'safe',
  start: 'start',
  proxy: 'proxy',
  skip: 'skip',
  copy: 'copy',
} as const;
export type TAction = (typeof ACTION)[keyof typeof ACTION];

export interface ICardLine {
  mark: TMark;
  title: string;
  rest: string;
  spin: boolean;
  action: { kind: TAction; label: string } | null;
  /** The quieter actions beside the main one («Пропустить»). */
  extra: readonly { kind: TAction; label: string }[];
  /** The item of the base package the line is about (its id without «base:»), for «Пропустить». */
  baseId: string;
  /** The command «Скопировать команду» copies. */
  command: string;
  /** The name of that item, for the notice. */
  baseName?: string;
}

export interface ICard {
  title: string;
  stages: readonly ICardStage[];
  /** The stage whose steps are shown: the picked one, else the current one. */
  shown: number;
  current: number;
  steps: readonly ICardStep[];
  more: number;
  run: { text: string; count: string; width: string } | null;
  line: ICardLine;
  done: boolean;
}

export const cardOf = (view: ISetupView, picked: number | null): ICard => {
  const groups = STAGES.map((s) => ({ ...s, items: view.items.filter((it) => it.kind === s.kind) })).filter(
    (g) => g.items.length > 0,
  );
  const marks = groups.map((g) => stageMark(g.items.map(markOf)));
  let current = marks.findIndex((m) => m === MARK.run || m === MARK.you || m === MARK.fail);
  if (current < 0) current = marks.findIndex((m) => m === MARK.wait);
  const shown = picked !== null && picked < groups.length ? picked : current;
  const stages = groups.map((g, i): ICardStage => {
    const done = g.items.filter((it) => it.status === SETUP_STATUS.ok).length;
    return {
      name: t(g.key),
      mark: marks[i] ?? MARK.wait,
      count: i === current ? `${done}/${g.items.length}` : '',
      linkDone: i > 0 && marks[i - 1] === MARK.done,
    };
  });
  const inside = shown >= 0 ? (groups[shown]?.items ?? []) : [];
  // The steps that need something first: a failed or wanted one is never hidden behind «ещё».
  const order = (m: TMark) => ({ fail: 0, you: 1, run: 2, wait: 3, done: 4 })[m];
  const sorted = [...inside].sort((a, b) => order(markOf(a)) - order(markOf(b)));
  const steps = sorted.slice(0, INSIDE_MAX).map((it) => ({ name: it.name, mark: markOf(it) }));

  const running = view.items.find((it) => it.status === SETUP_STATUS.running);
  let run: ICard['run'] = null;
  if (running) {
    const g = groups.find((x) => x.kind === running.kind);
    const total = g?.items.length ?? 1;
    const done = g?.items.filter((it) => it.status === SETUP_STATUS.ok).length ?? 0;
    run = {
      text: t(RUN_KEY[running.kind], { name: running.name }),
      count: t('next.setup.run.count', { done, total }),
      width: `${Math.round((done / total) * 100)}%`,
    };
  }

  const failed = view.items.find((it) => it.status === SETUP_STATUS.error);
  const wanted = view.items.find((it) => it.status === SETUP_STATUS.pending && it.secret);
  const waiting = view.items.some((it) => it.status === SETUP_STATUS.pending);
  let line: ICardLine;
  if (view.done) {
    line = {
      mark: MARK.done,
      title: '',
      rest: t('next.setup.line.done'),
      spin: false,
      action: null,
      extra: [],
      baseId: '',
      command: '',
    };
  } else if (failed) {
    line = {
      mark: MARK.fail,
      title: t('next.setup.line.failed', { name: failed.name }),
      rest: failed.message,
      spin: false,
      action: running ? null : { kind: ACTION.retry, label: t('next.setup.action.retry') },
      extra: [],
      baseId: '',
      command: '',
    };
  } else if (wanted) {
    line = {
      mark: MARK.you,
      title: t('next.setup.line.secret', { secret: wanted.secret }),
      rest: wanted.name,
      spin: false,
      action: { kind: ACTION.safe, label: t('next.setup.action.safe') },
      extra: [],
      baseId: '',
      command: '',
    };
  } else if (running) {
    line = {
      mark: MARK.run,
      title: t('next.setup.line.running'),
      rest: t('next.setup.line.runningRest'),
      spin: true,
      action: null,
      extra: [],
      baseId: '',
      command: '',
    };
  } else {
    line = {
      mark: MARK.wait,
      title: t('next.setup.line.waiting'),
      rest: waiting ? t('next.setup.line.waitingRest') : '',
      spin: false,
      action: { kind: ACTION.start, label: t('next.setup.action.start') },
      extra: [],
      baseId: '',
      command: '',
    };
  }
  // The base package first: a tool without network or one to install by hand, with what the person can do about it.
  const noNet = view.items.find((it) => it.base?.status === BASE_STATUS.noAccess);
  const byHand = view.items.find((it) => it.base?.command && it.status !== SETUP_STATUS.ok);
  const skip = (it: { itemId: string; name: string }) => ({
    extra: [{ kind: ACTION.skip, label: t('next.setup.action.skip') }],
    baseId: it.itemId.replace(/^base:/, ''),
    baseName: it.name,
  });
  if (!view.done && noNet && !(failed && failed.kind !== SETUP_KIND.base && failed !== noNet)) {
    line = {
      mark: MARK.you,
      title: noNet.base?.host
        ? t('next.setup.line.noAccessHost', { name: noNet.name, host: noNet.base.host })
        : t('next.setup.line.noAccess', { name: noNet.name }),
      rest: noNet.message,
      spin: false,
      action: { kind: ACTION.proxy, label: t('next.setup.action.proxy') },
      command: '',
      ...skip(noNet),
    };
  } else if (!view.done && byHand && !failed && !wanted) {
    line = {
      mark: MARK.you,
      title: t('next.setup.line.byHand', { name: byHand.name }),
      rest: view.baseCommand || (byHand.base?.command ?? ''),
      spin: false,
      action: { kind: ACTION.copy, label: t('next.setup.action.copy') },
      command: view.baseCommand || (byHand.base?.command ?? ''),
      ...skip(byHand),
    };
  }
  let titleKey = 'next.setup.title.first';
  if (view.done) titleKey = 'next.setup.title.done';
  else if (view.run) titleKey = 'next.setup.title.more';
  return {
    title: t(titleKey),
    stages,
    shown,
    current,
    steps,
    more: Math.max(0, inside.length - steps.length),
    run,
    line,
    done: view.done,
  };
};
