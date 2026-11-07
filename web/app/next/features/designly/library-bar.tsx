// The library's filter row: the kind, «Недавние», the design system or kit a project is built on, the sort, and
// the person's own tools (a new folder, select, the trash). Chips are toggles; the two choices are menus, not native selects.
import { Menu } from '@base-ui/react/menu';
import type { TDesignKind } from '@/next/features/designly/designly-home';
import { SORTS, type ILibraryFilters, type TSort } from '@/next/features/designly/library-model';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { t } from '@/next/i18n';

// Literals, not DESIGN_KIND: the home imports this file, and a value import back would be a cycle.
const KINDS: readonly TDesignKind[] = ['site', 'slides', 'video', 'ds'];

interface IPickProps<V extends string | null> {
  icon: string;
  label: string;
  /** What the trigger says; set when something is chosen. */
  value: string | null;
  options: { id: V; label: string }[];
  current: V;
  onPick: (id: V) => void;
  menuLabel: string;
}

const PickMenu = <V extends string | null>({
  icon,
  label,
  value,
  options,
  current,
  onPick,
  menuLabel,
}: IPickProps<V>) => {
  const actions = useMenuActions();
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger className="hx-dl__chip" data-on={value !== null} data-menu="true">
        <Icon name={icon} size="sm" />
        <span>{value ?? label}</span>
        <Icon name="chevron-down" size="sm" />
      </Menu.Trigger>
      <MenuPanel
        actions={actions}
        className="hx-cmenu"
        label={menuLabel}
        side="bottom"
        align="start"
        sideOffset={6}
      >
        {options.map((o) => (
          <Menu.Item key={o.id ?? 'any'} className="hx-cmenu__item" onClick={() => onPick(o.id)}>
            {o.id === current ? <Icon name="check" size="sm" /> : <span className="hx-dl__nocheck" />}
            <span className="hx-cmenu__txt">{o.label}</span>
          </Menu.Item>
        ))}
      </MenuPanel>
    </Menu.Root>
  );
};

export interface ILibraryBarProps {
  filters: ILibraryFilters;
  onFilters: (next: ILibraryFilters) => void;
  systems: { id: string; label: string }[];
  /** The person's own tools: absent where the library cannot be laid out (an older runtime). */
  tools: {
    selecting: boolean;
    trashOpen: boolean;
    onNewFolder: () => void;
    onSelect: () => void;
    onTrash: () => void;
  } | null;
}

const LibraryBar = ({ filters, onFilters, systems, tools }: ILibraryBarProps) => {
  const system = systems.find((s) => s.id === filters.system) ?? null;
  return (
    <div className="hx-dl__filters" role="group" aria-label={t('next.designly.filters')}>
      {KINDS.map((k: TDesignKind) => (
        <button
          key={k}
          type="button"
          className="hx-dl__chip"
          aria-pressed={filters.kind === k}
          onClick={() => onFilters({ ...filters, kind: filters.kind === k ? null : k })}
        >
          {t(`next.designly.kindFilter.${k}`)}
        </button>
      ))}
      <button
        type="button"
        className="hx-dl__chip"
        aria-pressed={filters.recent}
        onClick={() => onFilters({ ...filters, recent: !filters.recent })}
      >
        <Icon name="clock" size="sm" />
        {t('next.designly.recent')}
      </button>
      {systems.length > 0 && (
        <PickMenu<string | null>
          icon="palette"
          label={t('next.designly.system')}
          value={system?.label ?? null}
          current={filters.system}
          options={[
            { id: null, label: t('next.designly.systemAny') },
            ...systems.map((s) => ({ id: s.id, label: s.label })),
          ]}
          onPick={(id) => onFilters({ ...filters, system: id })}
          menuLabel={t('next.designly.system')}
        />
      )}
      <PickMenu<TSort>
        icon="arrow-up-down"
        label={t('next.designly.sortBy')}
        value={t(`next.designly.sort.${filters.sort}`)}
        current={filters.sort}
        options={SORTS.map((s) => ({ id: s, label: t(`next.designly.sort.${s}`) }))}
        onPick={(sort) => onFilters({ ...filters, sort })}
        menuLabel={t('next.designly.sortBy')}
      />
      <span className="hx-spacer" />
      {tools && (
        <>
          <button type="button" className="hx-dl__chip" onClick={tools.onNewFolder}>
            <Icon name="folder-plus" size="sm" />
            {t('next.designly.newFolder')}
          </button>
          <button
            type="button"
            className="hx-dl__chip"
            aria-pressed={tools.selecting}
            onClick={tools.onSelect}
          >
            <Icon name="list-checks" size="sm" />
            {t('next.designly.select')}
          </button>
          <button
            type="button"
            className="hx-dl__chip"
            aria-pressed={tools.trashOpen}
            onClick={tools.onTrash}
          >
            <Icon name="trash-2" size="sm" />
            {t('next.designly.trash')}
          </button>
        </>
      )}
    </div>
  );
};

export default LibraryBar;
