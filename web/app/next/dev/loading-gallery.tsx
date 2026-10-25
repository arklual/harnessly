// What does not load, and what takes the place of a skeleton: ?dev=loading with the installation's next-ui
// feature. ?scene=<id>&theme=light|dark&lang=en|ru pick one; the scenes without an id show the failure variants.
//
//   variants        the three sizes of the failed state (page, pane, inline) with their «Повторить»
//   designly-error  the library did not come
//   designly-swap   the skeleton, then the cards: window.__anims has the animations running after the swap
//   plan | files | machine   the right pane's tools that did not load
//   artifacts       the Artifacts tool: a skeleton, then the rows, the titles arriving one by one (?ms= sets the skeleton's length)
//   artifacts-error the links did not come, then they do; artifacts-titles: the titles did not come
//   older           the feed's top after an older page failed
//   feed-states     the feed on its way, older turns on their way, the start of the conversation, a feed catching up
//   limits          the composer's chip with limits that did not come
// Each retry answers after a pause and the scene goes on to its loaded state. Never part of a production path.
import '@/next/styles/tokens.css';
import '@/next/styles/base.css';
import '@/next/styles/shell.css';
import '@/next/styles/ui.css';
import '@/next/styles/chat.css';
import '@/next/styles/pane.css';
import '@/next/styles/charts.css';
import '@/next/styles/limits.css';
import '@/next/styles/artifacts.css';
import '@/next/styles/icons.css';
import '@/next/dev/loading-gallery.css';

import { useEffect, useState, type ReactNode } from 'react';
import { pinLocale } from '@/next/i18n';
import { useTheme } from '@/platform/theme';
import DesignlyHome, { DESIGN_KIND, type IDesignCard } from '@/next/features/designly/designly-home';
import Feed from '@/next/features/chat/feed';
import LimitsChip from '@/next/features/limits/limits-chip';
import FilesView from '@/next/features/pane/files-view';
import MachineView from '@/next/features/pane/machine-view';
import PlanView from '@/next/features/pane/plan-view';
import ArtifactsView from '@/next/features/artifacts/artifacts-view';
import { ARTIFACT_KIND, type IArtifactItem } from '@/next/features/artifacts/model';
import { resetMetaStore, setMetaFetch } from '@/next/features/artifacts/meta-store';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import type { FeedItem } from '@/next/features/chat/types';
import type { MachineProc } from '@/next/protocol/v2';
import type { TPending } from '@/next/ui/use-pending-list';

const params = new URLSearchParams(window.location.search);
const SCENE = params.get('scene') ?? 'variants';
const SWAP_AFTER_MS = Number(params.get('ms') ?? 1600);
const ANSWER_MS = 900;

const later = <T,>(value: T, ms = ANSWER_MS) =>
  new Promise<T>((resolve) => setTimeout(() => resolve(value), ms));

const Box = ({ label, children, tall = false }: { label: string; children: ReactNode; tall?: boolean }) => (
  <div className="hx-lg__box" data-tall={tall || undefined}>
    <div className="hx-lg__label">{label}</div>
    <div className="hx-lg__body">{children}</div>
  </div>
);

const cards = (now: number): IDesignCard[] =>
  [
    'Лендинг для фестиваля',
    'Слайды: итоги квартала',
    'Дизайн-система Acme',
    'Личный кабинет организатора',
    'Мобильное меню',
    'Письмо-приглашение',
  ].map((title, i) => ({
    id: `d${i}`,
    title,
    boards: 2 + i,
    updatedAt: now - (i + 1) * 3_600_000,
    image: null,
    kind: i === 1 ? DESIGN_KIND.slides : i === 2 ? DESIGN_KIND.ds : DESIGN_KIND.site,
    pinned: i === 0,
    drawing: false,
    owner: null,
  }));

const noop = () => undefined;
const VOICE = { recording: null, onCancel: noop, onFinish: noop };

/** Samples the animations that run on the page for a second after the cards came: a second entrance would show here. */
const watchAnimations = () => {
  const started = performance.now();
  const seen: { at: number; names: string[] }[] = [];
  (window as unknown as { __anims: typeof seen }).__anims = seen;
  const tick = () => {
    const at = Math.round(performance.now() - started);
    const names = document
      .getAnimations()
      .map((a) => (a as CSSAnimation).animationName)
      .filter((name): name is string => typeof name === 'string' && name !== '');
    seen.push({ at, names });
    if (at < 1200) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
};

const DesignlyScene = ({ fail }: { fail: boolean }) => {
  const [now] = useState(() => Date.now());
  const [list, setList] = useState<IDesignCard[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    if (fail) {
      const timer = setTimeout(() => setError('Нет связи с машиной'), 300);
      return () => clearTimeout(timer);
    }
    const timer = setTimeout(() => {
      setList(cards(now));
      watchAnimations();
    }, SWAP_AFTER_MS);
    return () => clearTimeout(timer);
  }, [fail, now]);
  return (
    <DesignlyHome
      cards={list}
      error={error}
      now={now}
      onPin={() => later(undefined)}
      onOpen={noop}
      onRetry={() => {
        setError(null);
        void later(null).then(() => setList(cards(now)));
      }}
      compose={{
        systems: [],
        onCreate: () => later(undefined),
        brief: '',
        onBrief: noop,
        picker: null,
        voice: VOICE,
      }}
    />
  );
};

const PLAN_TASKS = [
  { id: '1', subject: 'Прочитать как грузятся панели', status: 'completed' as const },
  { id: '2', subject: 'Свести ошибки к одному виду', status: 'in_progress' as const },
  { id: '3', subject: 'Проверить обрыв связи', status: 'pending' as const },
];

const PlanScene = () => {
  const [state, setState] = useState<'failed' | 'loading' | 'ready'>('failed');
  return (
    <PlanView
      tasks={state === 'ready' ? PLAN_TASKS : null}
      error={state === 'failed' ? 'Нет ответа от машины' : null}
      onRetry={() => {
        setState('loading');
        void later('ready' as const).then(setState);
      }}
    />
  );
};

const FilesScene = () => {
  const [state, setState] = useState<'failed' | 'loading' | 'ready'>('failed');
  return (
    <FilesView
      root="/home/coder/workspace/harnessly"
      path=""
      listing={
        state === 'ready'
          ? {
              path: '',
              entries: [
                { name: 'web', path: 'web', kind: 'directory' },
                { name: 'README.md', path: 'README.md', kind: 'file' },
              ],
              truncated: false,
            }
          : null
      }
      file={null}
      editor={null}
      error={state === 'failed' ? 'Нет связи с машиной' : null}
      opening="folder"
      onRetry={() => {
        setState('loading');
        void later('ready' as const).then(setState);
      }}
      onOpen={noop}
    />
  );
};

const DOCS = 'https://a.example.com';
const link = (
  patch: Partial<IArtifactItem> & Pick<IArtifactItem, 'url' | 'kind'>,
  at: number,
): IArtifactItem => ({
  key: null,
  label: null,
  fallback: 'Документ',
  where: patch.url.replace('https://', ''),
  github: false,
  readable: true,
  at,
  ...patch,
});
const ARTIFACT_ITEMS: IArtifactItem[] = [
  link({ url: `${DOCS}/a/release-plan`, kind: ARTIFACT_KIND.doc }, 9),
  link({ url: `${DOCS}/a/audit-q3`, kind: ARTIFACT_KIND.doc }, 8),
  link({ url: `${DOCS}/b/flow`, kind: ARTIFACT_KIND.board, fallback: 'Доска' }, 7),
  ...['3156', '3142', '3098', '3077', '2990'].map((n, i) =>
    link(
      {
        url: `https://tracker.example.com/issue/ABC-${n}`,
        kind: ARTIFACT_KIND.task,
        key: `ABC-${n}`,
        fallback: `ABC-${n}`,
      },
      6 - i,
    ),
  ),
  link(
    {
      url: 'https://git.example.com/web/app/-/merge_requests/491',
      kind: ARTIFACT_KIND.mr,
      key: 'app !491',
      fallback: 'app !491',
    },
    1,
  ),
  link(
    {
      url: 'https://example.com/changelog',
      kind: ARTIFACT_KIND.link,
      readable: false,
      fallback: 'example.com/changelog',
    },
    0,
  ),
];
const ARTIFACT_META: Record<string, unknown> = {
  [`${DOCS}/a/release-plan`]: {
    kind: 'artifact',
    title: 'План выката: админка и афродита',
    description: 'Что едет в пятницу и как откатить',
  },
  [`${DOCS}/a/audit-q3`]: {
    kind: 'artifact',
    title: 'Аудит нагрузки на ВМ за третий квартал',
    description: null,
  },
  [`${DOCS}/b/flow`]: { kind: 'artifact', title: 'Путь покупателя', description: null },
  'https://tracker.example.com/issue/ABC-3156': {
    kind: 'issue',
    key: 'ABC-3156',
    title: 'Новый интерфейс: доводка правой панели',
    state: 'In Progress',
    resolved: false,
    assignee: { name: 'Пётр Иванов', avatar: null },
  },
  'https://tracker.example.com/issue/ABC-3142': {
    kind: 'issue',
    key: 'ABC-3142',
    title: 'Переезд чата на v2-протокол',
    state: 'Testing',
    resolved: false,
    assignee: { name: 'Ваня Петров', avatar: null },
  },
  'https://tracker.example.com/issue/ABC-3098': {
    kind: 'issue',
    key: 'ABC-3098',
    title: 'Возвраты: заявка и документы',
    state: 'Done',
    resolved: true,
    assignee: { name: 'Олег Орлов', avatar: null },
  },
  'https://tracker.example.com/issue/ABC-3077': {
    kind: 'issue',
    key: 'ABC-3077',
    title: 'Лимиты подписки в шапке',
    state: 'Open',
    resolved: false,
    assignee: null,
  },
  'https://tracker.example.com/issue/ABC-2990': {
    kind: 'issue',
    key: 'ABC-2990',
    title: 'Сканер: офлайн-режим',
    state: 'Done',
    resolved: true,
    assignee: { name: 'Пётр Иванов', avatar: null },
  },
  'https://git.example.com/web/app/-/merge_requests/491': {
    kind: 'mr',
    title: 'Кэш зависимостей и сборка вне Docker',
    state: 'opened',
    project: 'web/app',
    iid: 491,
    draft: false,
  },
};

const ARTIFACT_SCENES = {
  artifacts: 'ok',
  'artifacts-error': 'links',
  'artifacts-titles': 'titles',
} as const;

const ArtifactsScene = ({ mode }: { mode: 'ok' | 'links' | 'titles' }) => {
  const [items, setItems] = useState<IArtifactItem[]>([]);
  const [complete, setComplete] = useState(false);
  const [failed, setFailed] = useState(mode === 'links');
  useEffect(() => {
    const born = Date.now();
    resetMetaStore();
    try {
      localStorage.removeItem('hx-next:artifacts:items:v1');
      localStorage.removeItem('hx-next:artifacts:meta:v1');
    } catch {
      // storage blocked
    }
    let n = 0;
    setMetaFetch(async (url) => {
      const asked = decodeURIComponent(url.split('url=')[1].split('&')[0]);
      await later(null, 350 + 420 * n++);
      // «titles»: the tracker does not answer for the first six seconds.
      if (mode === 'titles' && Date.now() - born < 6000) return new Response('', { status: 502 });
      return new Response(JSON.stringify(ARTIFACT_META[asked]));
    });
    if (mode === 'links') return () => setMetaFetch(null);
    const timer = setTimeout(() => {
      setItems(ARTIFACT_ITEMS);
      setComplete(true);
      watchAnimations();
    }, SWAP_AFTER_MS);
    return () => {
      clearTimeout(timer);
      setMetaFetch(null);
    };
  }, [mode]);
  return (
    <ArtifactsView
      machine="m1"
      convKey="gallery"
      items={items}
      complete={complete}
      failed={failed}
      onReload={() => {
        setFailed(false);
        setTimeout(() => {
          setItems(ARTIFACT_ITEMS);
          setComplete(true);
        }, ANSWER_MS);
      }}
      onOpen={(_item, _title, event) => event.preventDefault()}
    />
  );
};

const NO_PROCS: ReadonlySet<string> = new Set();
const NO_PENDING: ReadonlyMap<string, TPending> = new Map();

const MachineScene = () => (
  <MachineView
    name="runner-2"
    stats={null}
    error="Нет ответа от машины"
    onRetry={noop}
    pending={NO_PENDING}
    hidden={NO_PROCS}
    onStop={(proc: MachineProc) => void proc}
  />
);

const OlderScene = () => {
  const [startedAt] = useState(() => Date.now());
  return (
    <div className="hx-lg__body">
      <Feed
        items={[]}
        hasOlder
        loadingOlder={false}
        olderFailed="Нет связи с машиной"
        onLoadOlder={noop}
        density="compact"
        offline
        enterAfter={startedAt}
        machineName="runner-2"
        onOpenFile={noop}
        onLoadSteps={() => Promise.resolve(true)}
        onContinueTurn={() => Promise.resolve()}
        onRetryTurn={() => Promise.resolve()}
        onRetryMessage={() => Promise.resolve()}
        onDeleteMessage={() => Promise.resolve()}
      />
    </div>
  );
};

const Variants = () => (
  <div className="hx-lg" data-cols="2">
    <Box label="page">
      <LoadFailed
        size={LOAD_FAILED_SIZE.page}
        title="Не удалось загрузить библиотеку"
        reason="Нет связи с машиной"
        onRetry={() => later(null, 2500)}
      />
    </Box>
    <Box label="pane">
      <LoadFailed
        title="План не загрузился"
        reason="Нет ответа от машины"
        onRetry={() => later(null, 2500)}
      />
    </Box>
    <Box label="inline">
      <LoadFailed
        size={LOAD_FAILED_SIZE.inline}
        title="Чаты не загрузились"
        reason="Машина не отвечает"
        onRetry={() => later(null, 2500)}
      />
    </Box>
    <Box label="limits">
      <LimitsChip view={{ state: 'failed', provider: 'claude', onRetry: () => later(undefined) }} />
    </Box>
  </div>
);

const switchRow = (n: number): FeedItem => ({
  kind: 'switch',
  key: `switch:${n}`,
  at: n,
  from: { id: 'claude-opus-5-5', effort: null },
  to: { id: 'claude-sonnet-5-5', effort: n % 2 === 0 ? 'high' : null },
});
const ROWS: FeedItem[] = [switchRow(1), switchRow(2), switchRow(3)];

const FeedState = ({
  items,
  hasOlder = false,
  loadingOlder = false,
  catchingUp = false,
}: {
  items: FeedItem[] | null;
  hasOlder?: boolean;
  loadingOlder?: boolean;
  catchingUp?: boolean;
}) => (
  <div className="hx-lg__body">
    <Feed
      items={items}
      hasOlder={hasOlder}
      loadingOlder={loadingOlder}
      catchingUp={catchingUp}
      onLoadOlder={noop}
      density="compact"
      offline={false}
      enterAfter={Number.MAX_SAFE_INTEGER}
      machineName="runner-2"
      onContinueTurn={() => Promise.resolve()}
      onRetryTurn={() => Promise.resolve()}
      onRetryMessage={() => Promise.resolve()}
      onDeleteMessage={() => Promise.resolve()}
    />
  </div>
);

const Gallery = () => {
  const { setTheme } = useTheme();
  useEffect(() => {
    const theme = params.get('theme');
    if (theme === 'dark' || theme === 'light') setTheme(theme);
    const lang = params.get('lang');
    pinLocale(lang === 'en' || lang === 'ru' ? lang : null);
  }, [setTheme]);

  if (SCENE === 'designly-error' || SCENE === 'designly-swap')
    return (
      <div className="hx-lg">
        <Box label={SCENE} tall>
          <DesignlyScene fail={SCENE === 'designly-error'} />
        </Box>
      </div>
    );
  if (SCENE === 'artifacts' || SCENE === 'artifacts-error' || SCENE === 'artifacts-titles')
    return (
      <div className="hx-lg">
        <Box label={SCENE} tall>
          <ArtifactsScene mode={ARTIFACT_SCENES[SCENE]} />
        </Box>
      </div>
    );
  if (SCENE === 'feed-states')
    return (
      <div className="hx-lg" data-cols="2">
        <Box label="loading" tall>
          <FeedState items={null} />
        </Box>
        <Box label="older loading" tall>
          <FeedState items={ROWS} hasOlder loadingOlder />
        </Box>
        <Box label="start" tall>
          <FeedState items={ROWS} />
        </Box>
        <Box label="catching up" tall>
          <FeedState items={ROWS} hasOlder catchingUp />
        </Box>
      </div>
    );
  if (SCENE === 'panes')
    return (
      <div className="hx-lg" data-cols="2">
        <Box label="plan">
          <PlanScene />
        </Box>
        <Box label="files">
          <FilesScene />
        </Box>
        <Box label="machine">
          <MachineScene />
        </Box>
        <Box label="older" tall>
          <OlderScene />
        </Box>
      </div>
    );
  return <Variants />;
};

export default Gallery;
