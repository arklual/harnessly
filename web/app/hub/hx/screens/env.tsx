// The «Окружение» screen of the board «Хаб»: what a machine gets installed and set up, layer by layer (organization,
// tenant, team). It follows the board «Окружение — первая версия»: the banner of what this is, «Настраиваете ▾»,
// the layers with their items by kind, and a window to add, change, copy, check in a sandbox and read the logs.
// One implementation for every width and every role: a member sees the same layers, only to read.
import { useEffect, useMemo, useRef, useState } from 'react';

import type { ApplyWhen, EnvEntryRow } from '../../../hub-api/env';
import { useOrg } from '../../org/context';
import { useClosing } from '../../org/ui/closing';
import { Titles } from '../shell';
import { Btn, closingAttr, Icon } from '../ui';
import '../styles/env.css';
import { reposOf, useActions } from './env/actions';
import { AddHome, AddKind, AddLink } from './env/add';
import { ApplyMenu } from './env/apply';
import { Empty, isNotEnabled, LoadFailed, Search, Skeleton } from './env/bits';
import { type SheetCtx, type SheetMode } from './env/ctx';
import { CopyBody } from './env/copy';
import { ItemBody } from './env/item';
import { type LayerActions, LayerCard, type LayerVM } from './env/layers';
import { LogsBody } from './env/logs';
import { MachineStrip } from './env/machine';
import { GROUP_ORDER, KIND } from './env/model';
import { ScopePicker } from './env/picker';
import { TestBody } from './env/sandbox';
import { blocksOf, chainOf, firstTarget, keyOf, metaOf, same, type Target } from './env/scope';
import { Sheet } from './env/sheet';
import { useCatalog, useEnvData } from './env/state';
import { Toast, useToast } from './env/toast';

const INTRO_KEY = 'hl-env-intro-hidden';
const FRESH_MS = 2600;

const readIntro = (): boolean => {
  try {
    return localStorage.getItem(INTRO_KEY) !== '1';
  } catch {
    return true;
  }
};
const writeIntro = (on: boolean) => {
  try {
    localStorage.setItem(INTRO_KEY, on ? '0' : '1');
  } catch {
    // not remembered
  }
};

const introText = (role: 'admin' | 'lead' | 'member'): { title: string; text: string } => {
  if (role === 'member')
    return {
      title: 'Что будет на вашей машине',
      text: 'Ваша машина ставит всё из списка сама: от организации, тенанта и вашей команды. С этим работают агенты в ваших чатах. Поменять список могут редакторы команды и админ.',
    };
  if (role === 'admin')
    return {
      title: 'Что будет на машинах людей',
      text: 'Человек подключает машину к хабу, и она ставит всё из своей организации, тенанта и команды. Агенты в его чатах сразу работают с нужными репозиториями и доступами. Правка уровня обновит все машины под ним.',
    };
  return {
    title: 'Что будет на машине каждого в команде',
    text: 'Человек подключает машину к хабу, и она ставит всё из списка. Агенты в его чатах сразу работают с нужными репозиториями и доступами. Поменяете здесь — обновится у всех в команде.',
  };
};

const SHEET_LABEL: Record<SheetMode['mode'], string> = {
  add: 'Добавить в окружение',
  edit: 'Пункт окружения',
  view: 'Пункт окружения',
  copy: 'Скопировать из другой команды',
  test: 'Проверить в песочнице',
  logs: 'Логи',
};

const Screen = () => {
  const { shell, scope } = useOrg();
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const role = shell.viewer.role;
  const member = role === 'member';
  const data = useEnvData();
  const { layers } = data;
  const [pick, setPick] = useState<Target | null>(null);
  const [q, setQ] = useState('');
  const [openMap, setOpenMap] = useState<Record<string, boolean>>({});
  const [sheet, setSheet] = useState<SheetMode | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const [intro, setIntro] = useState(readIntro);
  const introAnim = useClosing(intro, 300);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(new Set());
  const toast = useToast();
  const timers = useRef<number[]>([]);
  useEffect(() => {
    const list = timers.current;
    return () => list.forEach((t) => window.clearTimeout(t));
  }, []);

  const target: Target | null = useMemo(() => {
    if (!layers) return null;
    if (pick && layers.some((l) => same(l, pick))) return pick;
    return firstTarget(layers, role, shell.viewer.teamId);
  }, [layers, pick, role, shell.viewer.teamId]);

  const catalog = useCatalog(!member && layers !== null, target);
  const actions = useActions(layers ?? [], data.refresh, catalog.refresh, toast);

  if (data.error != null && layers === null) {
    return (
      <>
        <Header />
        {isNotEnabled(data.error) ? (
          <Empty>Окружение в организации ещё не включено</Empty>
        ) : (
          <LoadFailed error={data.error} onRetry={data.reload} />
        )}
      </>
    );
  }
  if (!layers || !target) {
    return (
      <>
        <Header />
        <Skeleton />
      </>
    );
  }

  const chain = chainOf(layers, target);
  const meta = metaOf(chain[chain.length - 1] ?? { layer: target.layer, title: '' });
  const view = { layers, teams: data.view?.teams ?? [] };
  const canEdit = !member && chain.some((l) => same(l, target) && !l.locked);
  const repos = reposOf(chain);
  const secretNames = [...new Set((catalog.items ?? []).flatMap((c) => c.source.secrets ?? []))].sort();

  const flash = (ids: string[]) => {
    setFresh((f) => new Set([...f, ...ids]));
    timers.current.push(
      window.setTimeout(() => setFresh((f) => new Set([...f].filter((x) => !ids.includes(x)))), FRESH_MS),
    );
  };
  const openSheet = (m: SheetMode) => {
    setSheet(m);
    setSheetOpen(true);
  };
  const closeSheet = () => {
    setSheetOpen(false);
    timers.current.push(window.setTimeout(() => setSheet(null), 220));
  };
  const ctx: SheetCtx = {
    role,
    view,
    layers,
    target,
    setTarget: (t) => {
      setPick(t);
      setOpenMap({});
      setQ('');
    },
    chain,
    meta,
    catalog: catalog.items,
    repos,
    secretNames,
    orgAdmin: layers.some((l) => l.layer === 'org' && !l.locked),
    actions,
    go: (m) => setSheet(m),
    close: closeSheet,
    canCopy: !member && meta.kind === 'team' && layers.some((l) => l.layer === 'team' && !same(l, target)),
    flash,
  };

  const needle = q.trim();
  const vms = chain.map((layer, index): { vm: LayerVM; blocks: ReturnType<typeof blocksOf> } => {
    const m = metaOf(layer);
    const edit = canEdit && same(layer, target);
    const blocks = blocksOf(layer.items, q);
    let sub = edit
      ? `Ставится каждому в ${m.loc} · меняете вы`
      : `Общее для ${m.kind === 'org' ? 'всей организации' : m.gen} · ${role === 'admin' ? 'уровень выше' : 'меняет админ'}`;
    if (member && same(layer, target)) sub = `Ставится каждому в ${m.loc} · меняют редакторы команды`;
    const open = needle !== '' ? blocks.length > 0 : (openMap[keyOf(layer)] ?? same(layer, target));
    return { vm: { layer, meta: m, index, open, edit, sub }, blocks };
  });
  const shownLayers = needle === '' ? vms : vms.filter((x) => x.blocks.length > 0);
  const actionsOf = (vm: LayerVM): LayerActions => ({
    onToggle: () => setOpenMap({ ...openMap, [keyOf(vm.layer)]: !vm.open }),
    onAdd: () => openSheet({ mode: 'add', step: 'home' }),
    onOpen: member
      ? null
      : (row: EnvEntryRow) =>
          openSheet({ mode: vm.edit && !row.locked ? 'edit' : 'view', entryId: row.entryId }),
    onPin: (row) => void actions.pin(row),
    onReorder: (ids) => actions.reorder({ layer: vm.layer.layer, scopeId: vm.layer.scopeId }, ids),
    groups: {
      onAdd: (name) => actions.addGroup({ layer: vm.layer.layer, scopeId: vm.layer.scopeId }, name),
      onPatch: actions.patchGroup,
      onRemove: actions.removeGroup,
      onMove: actions.moveToGroup,
      onMine: member ? (g, on) => actions.mineGroup(g.id, g.name, on, space) : null,
    },
  });

  const text = introText(role);
  return (
    <>
      <Header
        introHidden={!intro}
        onIntro={() => {
          writeIntro(true);
          setIntro(true);
        }}
        onLogs={() => openSheet({ mode: 'logs' })}
        onTest={canEdit ? () => openSheet({ mode: 'test' }) : null}
        onApply={canEdit ? (when) => actions.applyMachines(target, when) : null}
        onAdd={canEdit ? () => openSheet({ mode: 'add', step: 'home' }) : null}
      />
      {introAnim.mounted && (
        <div className="ev-intro-w" data-closing={closingAttr(introAnim.closing)} data-block="Что это">
          <div>
            <div className="ev-intro">
              <span className="ev-intro__ic">
                <Icon i="layers" />
              </span>
              <div className="ev-intro__t">
                <b>{text.title}</b>
                <p>{text.text}</p>
                <div className="ev-kinds">
                  {GROUP_ORDER.filter((k) => k !== 'doc').map((k) => (
                    <span className="ev-kind" key={k}>
                      <Icon i={KIND[k].icon} size="sm" />
                      {KIND[k].one}
                    </span>
                  ))}
                </div>
              </div>
              <Btn
                variant="ghost"
                size="sm"
                shape="icon"
                aria-label="Скрыть"
                onClick={() => {
                  writeIntro(false);
                  setIntro(false);
                }}
              >
                <Icon i="x" />
              </Btn>
            </div>
          </div>
        </div>
      )}
      {member && (
        <MachineStrip machines={data.machines} refresh={data.refresh} say={toast.say} fail={toast.fail} />
      )}
      <div className="ev-bar" data-block="Что настраиваете и поиск">
        <ScopePicker
          view={view}
          role={role}
          target={target}
          onPick={ctx.setTarget}
          variant="page"
          label={member ? 'Ваша команда' : 'Настраиваете'}
          disabled={member || (role !== 'admin' && view.layers.filter((l) => !l.locked).length <= 1)}
        />
        <Search
          value={q}
          onChange={setQ}
          placeholder="Найти репозиторий, скилл, MCP"
          label="Найти в окружении"
        />
      </div>
      <div className="ev-layers" data-block="Уровни">
        {shownLayers.map(({ vm, blocks }) => (
          <LayerCard key={keyOf(vm.layer)} vm={vm} rows={blocks} q={q} a={actionsOf(vm)} fresh={fresh} />
        ))}
      </div>
      {shownLayers.length === 0 && (
        <div className="ev-empty">Ничего не нашлось. Попробуйте другое слово или добавьте ссылкой</div>
      )}
      {sheet && (
        <Sheet open={sheetOpen} label={SHEET_LABEL[sheet.mode]} onClose={closeSheet}>
          {sheet.mode === 'add' && sheet.step === 'home' && <AddHome ctx={ctx} />}
          {sheet.mode === 'add' && sheet.step === 'link' && <AddLink ctx={ctx} />}
          {sheet.mode === 'add' && sheet.step === 'kind' && (
            <AddKind key={sheet.kind} ctx={ctx} kind={sheet.kind} start={sheet.draft} />
          )}
          {(sheet.mode === 'edit' || sheet.mode === 'view') && (
            <ItemBody key={sheet.entryId} ctx={ctx} entryId={sheet.entryId} edit={sheet.mode === 'edit'} />
          )}
          {sheet.mode === 'copy' && <CopyBody ctx={ctx} />}
          {sheet.mode === 'test' && <TestBody ctx={ctx} />}
          {sheet.mode === 'logs' && <LogsBody ctx={ctx} />}
        </Sheet>
      )}
      <Toast toast={toast.toast} onClose={toast.hide} />
    </>
  );
};

const Header = ({
  introHidden,
  onIntro,
  onLogs,
  onTest,
  onApply,
  onAdd,
}: {
  introHidden?: boolean;
  onIntro?: () => void;
  onLogs?: () => void;
  onTest?: (() => void) | null;
  onApply?: ((when: ApplyWhen) => Promise<boolean>) | null;
  onAdd?: (() => void) | null;
}) => (
  <div className="ad-head" data-block="Заголовок">
    <Titles
      title="Окружение"
      end={
        introHidden ? (
          <span className="hx-tipped">
            <Btn
              className="ev-what"
              variant="ghost"
              size="sm"
              shape="icon"
              aria-label="Что это за раздел"
              onClick={onIntro}
            >
              <Icon i="info" />
            </Btn>
            <span className="hx-tip pz-tip" data-align="start">
              Что это за раздел
            </span>
          </span>
        ) : undefined
      }
    />
    <div className="ad-tools ev-tools">
      {onLogs && (
        <Btn variant="ghost" size="sm" onClick={onLogs} aria-label="Логи">
          <Icon i="file-text" size="sm" />
          <span className="ev-tl">Логи</span>
        </Btn>
      )}
      {onTest && (
        <Btn size="sm" onClick={onTest} aria-label="Проверить в песочнице">
          <Icon i="shield-check" size="sm" />
          <span className="ev-tl">Проверить</span>
        </Btn>
      )}
      {onApply && <ApplyMenu onApply={onApply} />}
      {onAdd && (
        <Btn variant="primary" size="sm" icon="plus" onClick={onAdd}>
          Добавить
        </Btn>
      )}
    </div>
  </div>
);

export default Screen;
