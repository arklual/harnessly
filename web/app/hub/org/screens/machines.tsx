// Машины (HRN-539): the machines of a tenant by team, or of the whole organization by tenant — the ones with trouble
// first; a card in a sheet; restart, resources, access and disable on it.
import '../machines.css';

import { useMemo, useRef, useState } from 'react';
import {
  Gauge,
  MessagesSquare,
  Plus,
  Search,
  Server,
  TriangleAlert,
  Activity,
  ArrowUpRight,
  ChevronDown,
  Check,
} from 'lucide-react';

import { Button, Popup } from '@kit';

import { useNow } from '../../lib/hooks';
import { navigate, useQuery } from '../../lib/router';
import type { MachineGroup, MachinesList } from '../api/machines';
import { useOrg } from '../context';
import { useLoad } from '../hooks';
import { Loading, LoadError, Page, PageHead, Seg } from '../ui/page';
import { MiniBars, ProgressLine, Tile } from '../ui/tile';
import { useClosing } from '../ui/closing';
import { plural } from './machines/format';
import { FlatList, GroupBlock } from './machines/list';
import { AddMachineSheet } from './machines/add';
import { MachineSheet } from './machines/sheet';

type View = 'groups' | 'list';

const Tiles = ({ l, org }: { l: MachinesList; org: boolean }) => {
  const s = l.summary;
  const mach = (n: number) => plural(n, 'машина', 'машины', 'машин');
  const num = (n: number) => n.toLocaleString('ru-RU');
  const trouble: string[] = [];
  if (s.diskHot && s.diskHot.pct >= 85) trouble.push(`диск ${s.diskHot.name} ${s.diskHot.pct}%`);
  if (s.outdatedRunners > 0)
    trouble.push(
      `${s.outdatedRunners} ${plural(s.outdatedRunners, 'раннер', 'раннера', 'раннеров')} ${s.outdatedRunners === 1 ? 'устарел' : 'устарели'}`,
    );
  const down = s.offline;
  return (
    <div className="ho-tiles">
      <Tile
        tone="green"
        icon={<Server size={15} />}
        title="На связи"
        viz={<MiniBars values={l.summary.onlineSeries} />}
        line={`${num(s.online)} из ${num(s.total)}${s.idleWeek ? ` · ${s.idleWeek === 1 ? 'одна простаивает' : `${s.idleWeek} простаивают`}` : ''}`}
      />
      <Tile
        tone="violet"
        icon={<MessagesSquare size={15} />}
        title="Чаты сейчас"
        viz={<MiniBars values={s.chatsSeries} />}
        line={`${num(s.chatsNow)} идут${s.chatsToday === null ? '' : ` · ${num(s.chatsToday)} за сегодня`}`}
      />
      {org ? (
        <>
          <Tile
            tone="amber"
            icon={<Gauge size={15} />}
            title="Простаивают неделю"
            viz={<ProgressLine value={s.total ? s.idleWeek / s.total : 0} />}
            line={`${s.idleWeek} ${mach(s.idleWeek)}`}
          />
          <Tile
            tone="rose"
            icon={<TriangleAlert size={15} />}
            title="Не на связи"
            viz={<ProgressLine value={s.total ? down / s.total : 0} />}
            line={`${down} ${mach(down)} в ${s.tenantsOffline} ${plural(s.tenantsOffline, 'тенанте', 'тенантах', 'тенантах')}`}
          />
        </>
      ) : (
        <>
          <Tile
            tone="teal"
            icon={<Gauge size={15} />}
            title="Нагрузка"
            viz={<MiniBars values={s.loadSeries} />}
            line={
              s.cpuMean === null
                ? 'нет данных'
                : `в среднем ${s.cpuMean}%${s.hottest && s.hottest.pct >= 80 ? ` · ${s.hottest.name} под ${s.hottest.pct}%` : ''}`
            }
          />
          <Tile
            tone="amber"
            icon={<TriangleAlert size={15} />}
            title="Требует внимания"
            viz={<ProgressLine value={s.total ? (down + s.outdatedRunners) / s.total : 0} />}
            line={
              [down ? `${down} не на связи` : null, ...trouble].filter(Boolean).join(' · ') || 'всё в порядке'
            }
          />
        </>
      )}
    </div>
  );
};

const TenantFilter = ({
  tenants,
  value,
  onChange,
}: {
  tenants: { slug: string; name: string }[];
  value: string;
  onChange: (v: string) => void;
}) => {
  const [open, setOpen] = useState(false);
  const anchor = useRef<HTMLButtonElement>(null);
  const { mounted, closing } = useClosing(open, 140);
  const cur = tenants.find((t) => t.slug === value);
  return (
    <>
      <button
        ref={anchor}
        type="button"
        className="hm-filter"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        {cur?.name ?? 'Все тенанты'}
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      <Popup anchor={anchor} open={mounted} onClose={() => setOpen(false)} className="ho-pop" role="listbox">
        <div className="ho-pop__in" data-closing={closing || undefined}>
          {[{ slug: '', name: 'Все тенанты' }, ...tenants].map((t) => (
            <button
              key={t.slug}
              type="button"
              role="option"
              aria-selected={t.slug === value}
              className="ho-pop__item"
              onClick={() => {
                onChange(t.slug);
                setOpen(false);
              }}
            >
              <span className="ho-pop__name">{t.name}</span>
              {t.slug === value && <Check size={14} aria-label="Выбрано" />}
            </button>
          ))}
        </div>
      </Popup>
    </>
  );
};

const matches = (q: string, g: MachineGroup['machines'][number]) =>
  [g.name, g.ownerName, g.purpose, g.tenantName, g.teamName].some((v) => v?.toLowerCase().includes(q));

export const MachinesScreen = () => {
  const { scope, shell, base } = useOrg();
  const org = scope.kind === 'org';
  const slug = org ? 'all' : scope.tenant.slug;
  const { data, error, reload, refresh } = useLoad(
    (api) => api.machinesList(shell.org.slug, { scope: slug }),
    [shell.org.slug, slug],
  );
  const query = useQuery();
  const openId = query.get('machine');
  const [kept, setKept] = useState(openId);
  if (openId && openId !== kept) setKept(openId);
  const [adding, setAdding] = useState(false);
  const addable = org ? shell.tenants : [scope.tenant];
  const canAdd = shell.viewer.role === 'admin' && addable.length > 0;
  const [view, setView] = useState<View>('groups');
  const [q, setQ] = useState('');
  const [tenant, setTenant] = useState('');
  const [opened, setOpened] = useState<Record<string, boolean>>({});
  const now = useNow(60_000);

  const needle = q.trim().toLowerCase();
  const groups = useMemo(() => {
    if (!data) return null;
    return data.groups
      .filter((g) => !tenant || g.tenantSlug === tenant)
      .map((g) => ({ ...g, machines: needle ? g.machines.filter((m) => matches(needle, m)) : g.machines }))
      .filter((g) => !needle || g.machines.length > 0);
  }, [data, needle, tenant]);
  const flat = useMemo(() => (groups ? groups.flatMap((g) => g.machines) : null), [groups]);
  const found = data?.groups.flatMap((g) => g.machines).find((m) => m.id === kept)?.name;
  const [lastName, setLastName] = useState('');
  if (found && found !== lastName) setLastName(found);
  const named = found ?? lastName;
  const open = (id: string) => navigate(`${base}/machines?machine=${encodeURIComponent(id)}`);
  const close = () => navigate(`${base}/machines`);
  const total = data?.summary.total ?? 0;
  const sub = org
    ? `${shell.org.name} · по тенантам, сначала те, где что-то не так`
    : `Тенант ${scope.tenant.name} · ${total} ${plural(total, 'машина', 'машины', 'машин')}`;

  return (
    <Page>
      <PageHead
        title={org ? 'Машины организации' : 'Машины'}
        sub={sub}
        actions={
          canAdd ? (
            <Button appearance="outline" size="s" icon={<Plus size={14} />} onClick={() => setAdding(true)}>
              Добавить машину
            </Button>
          ) : undefined
        }
      />
      {error != null ? (
        <LoadError error={error} retry={reload} />
      ) : !data || !groups || !flat ? (
        <Loading />
      ) : (
        <>
          <Tiles l={data} org={org} />
          <div className="hm-bar">
            <label className="hm-search">
              <Search size={14} aria-hidden="true" />
              <input
                type="search"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder={org ? 'Найти машину, тенант или человека' : 'Найти машину или человека'}
                aria-label="Поиск"
              />
            </label>
            {org && data.tenants.length > 0 && (
              <TenantFilter tenants={data.tenants} value={tenant} onChange={setTenant} />
            )}
            {data.mlCoreUrl && (
              <a
                className="hm-ext hm-ext--bar"
                href={data.mlCoreUrl}
                target="_blank"
                rel="noreferrer noopener"
              >
                <span className="hm-ext__ic">
                  <Activity size={14} />
                </span>
                <span className="hm-ext__t">
                  <b>Подробнее в ML Core</b>
                  <small>{org ? 'графики и логи всех машин' : 'графики и логи машин тенанта'}</small>
                </span>
                <ArrowUpRight size={14} aria-hidden="true" />
              </a>
            )}
            <Seg
              label="Вид"
              value={view}
              onChange={setView}
              options={[
                { value: 'groups', label: org ? 'По тенантам' : 'По командам' },
                { value: 'list', label: 'Списком' },
              ]}
            />
          </div>
          {flat.length === 0 && data.groups.length > 0 ? (
            <p className="hm-empty">Ничего не нашлось.</p>
          ) : data.groups.length === 0 ? (
            <p className="hm-empty">Машин пока нет. Добавьте первую, и она появится здесь.</p>
          ) : view === 'list' ? (
            <FlatList machines={flat} now={now} onOpen={open} />
          ) : (
            <div className="hm-groups">
              {groups.map((g, i) => (
                <GroupBlock
                  key={g.key}
                  g={g}
                  now={now}
                  base={base}
                  open={opened[g.key] ?? (!!needle || (groups.length === 1 && i === 0))}
                  onToggle={() =>
                    setOpened({
                      ...opened,
                      [g.key]: !(opened[g.key] ?? (!!needle || (groups.length === 1 && i === 0))),
                    })
                  }
                  onOpen={open}
                />
              ))}
            </div>
          )}
        </>
      )}
      <AddMachineSheet open={adding} tenants={addable} onClose={() => setAdding(false)} onAdded={refresh} />
      <MachineSheet
        id={kept}
        name={named}
        open={openId !== null}
        mlUrl={data?.mlCoreUrl ?? null}
        onClose={close}
        onChanged={refresh}
      />
    </Page>
  );
};
