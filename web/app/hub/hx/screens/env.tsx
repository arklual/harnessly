// The «Окружение» screen of the board «Хаб». Its own chunk: it loads when the person opens the section.
import { type ReactNode, useEffect, useMemo, useState } from 'react';

import { useOrg } from '../../org/context';
import { orgErrorText } from '../../org/api/errors';
import { Titles } from '../shell';
import { Btn } from '../ui';
import '../styles/env.css';
import { CardDrawer } from './env/card';
import { CatalogDrawer } from './env/catalog';
import {
  catalogTarget,
  flatRows,
  groupsOf,
  isFiltering,
  type KindFilter,
  withPatch,
  withRow,
} from './env/helpers';
import { FlatList, GroupCard } from './env/layers';
import { MemberEnv } from './env/member';
import { Empty, EnvSkeleton, isNotEnabled, LoadFailed, Tiles } from './env/parts';
import { KindSegment, SearchField, Segment } from './env/search';
import { useEnvView } from './env/state';
import { Toast, useToast } from './env/toast';
import { useEnvUrl } from './env/url';

type ViewMode = 'layers' | 'flat';
const VIEWS: { id: ViewMode; label: string }[] = [
  { id: 'layers', label: 'По слоям' },
  { id: 'flat', label: 'Списком' },
];

const ManageEnv = () => {
  const { api, shell, scope } = useOrg();
  const state = useEnvView();
  const { view, error, reload } = state;
  const url = useEnvUrl();
  const toast = useToast();
  const role = shell.viewer.role;
  const tenant = scope.kind === 'tenant' ? scope.tenant : null;

  const [kind, setKind] = useState<KindFilter>('all');
  const [q, setQ] = useState('');
  const [mode, setMode] = useState<ViewMode>('layers');
  const [opened, setOpened] = useState<Record<string, boolean>>({});

  const filter = useMemo(() => ({ kind, q }), [kind, q]);
  // «Last used» is relative to the moment the data came.
  // eslint-disable-next-line react-hooks/purity, react-hooks/exhaustive-deps
  const now = useMemo(() => Date.now(), [view]);
  const groups = useMemo(
    () => (view ? groupsOf(view, filter, opened, now) : []),
    [view, filter, opened, now],
  );
  const rows = useMemo(
    () => (view && mode === 'flat' ? flatRows(view, filter, now) : []),
    [view, filter, now, mode],
  );

  // A link to an entry that is not there (gone, or not in this scope) is cleaned off the address.
  const { es } = url;
  useEffect(() => {
    if (!view || !es) return;
    if (!view.layers.some((l) => l.items.some((r) => r.entryId === es))) url.close();
  }, [view, es]); // eslint-disable-line react-hooks/exhaustive-deps

  const target = catalogTarget(
    role,
    { orgId: shell.org.id, tenantId: tenant?.id ?? null },
    shell.viewer.teamId,
  );
  const targetTitle =
    view && target
      ? (view.layers.find((l) => l.layer === target.layer && l.scopeId === target.scopeId)?.title ?? '')
      : '';
  const catalogOn = shell.features === undefined || shell.features.includes('catalog');

  const flip = async (entryId: string, on: boolean) => {
    const res = await state.write({
      apply: (v) => withPatch(v, entryId, { enabled: on }),
      undo: (v) => withPatch(v, entryId, { enabled: !on }),
      run: () => api.envPatch(shell.org.slug, entryId, { enabled: on }),
      settle: (v, row) => withRow(v, row),
    });
    if (!res.ok) toast.show(orgErrorText(res.error));
  };

  const toggleOpen = (key: string) =>
    setOpened((o) => ({ ...o, [key]: !(isFiltering(filter) ? true : (o[key] ?? false)) }));

  const empty = view !== null && view.layers.every((l) => l.items.length === 0);
  const nothing = view !== null && !empty && (mode === 'layers' ? groups.length === 0 : rows.length === 0);
  const lead = tenant
    ? `Что получает каждый чат в тенанте ${tenant.name}. Складывается из организации, тенанта и команды`
    : 'Что получает каждый чат в организации. Складывается из организации, тенанта и команды';

  let body: ReactNode;
  if (error != null) {
    body = isNotEnabled(error) ? (
      <Empty>Здесь пока ничего нет</Empty>
    ) : (
      <LoadFailed error={error} onRetry={reload} />
    );
  } else if (!view) {
    body = <EnvSkeleton />;
  } else {
    body = (
      <div className="mo">
        <Tiles view={view} />
        {empty ? (
          <Empty>Здесь пока ничего нет</Empty>
        ) : (
          <>
            <div className="mo-bar">
              <SearchField
                value={q}
                onChange={setQ}
                placeholder="Найти скилл, MCP, репозиторий"
                label="Найти в окружении"
              />
              <KindSegment value={kind} onChange={setKind} />
              <span className="hx-spacer" />
              <Segment items={VIEWS} value={mode} onChange={setMode} label="Как показать" />
            </div>
            {nothing && <Empty>Ничего не нашлось</Empty>}
            {!nothing && mode === 'layers' && (
              <div className="mo-list">
                {groups.map((g) => (
                  <GroupCard
                    key={g.key}
                    g={g}
                    onToggleOpen={toggleOpen}
                    onOpen={url.openEntry}
                    onToggle={flip}
                  />
                ))}
              </div>
            )}
            {!nothing && mode === 'flat' && <FlatList rows={rows} onOpen={url.openEntry} onToggle={flip} />}
          </>
        )}
      </div>
    );
  }

  return (
    <>
      <div className="ad-head">
        <Titles title="Окружение" lead={lead} />
        {view && target && catalogOn && (
          <div className="ad-tools">
            <Btn variant="primary" size="sm" icon="plus" onClick={url.openCatalog}>
              Из каталога
            </Btn>
          </div>
        )}
      </div>
      {body}
      {view && (
        <>
          <CardDrawer
            entryId={url.es}
            view={view}
            state={state}
            canPin={role === 'admin'}
            onClose={url.close}
            onProblem={toast.show}
          />
          <CatalogDrawer
            open={url.catalog && target !== null}
            target={target}
            targetTitle={targetTitle}
            view={view}
            state={state}
            onClose={url.close}
            onProblem={toast.show}
          />
        </>
      )}
      <Toast text={toast.text} id={toast.n} onClose={toast.hide} />
    </>
  );
};

const Screen = () => {
  const { shell } = useOrg();
  return shell.viewer.role === 'member' ? <MemberEnv /> : <ManageEnv />;
};

export default Screen;
