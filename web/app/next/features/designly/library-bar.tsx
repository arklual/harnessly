// The library's tools in the toolbar's one row (was a second row of chips in HRN-860): a filter button whose menu
// holds the kind, «Недавние», the design system and the sort, and a «⋯» with the person's own tools (a new folder,
// select, the trash). A dot on the filter says something other than the default is chosen.
import { Menu } from '@base-ui/react/menu';
import type { ReactNode } from 'react';
import type { TDesignKind } from '@/next/features/designly/designly-home';
import { SORT, SORTS, isNarrowed, type ILibraryFilters } from '@/next/features/designly/library-model';
import Icon from '@/next/ui/icon';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import { t } from '@/next/i18n';

// Literals, not DESIGN_KIND: the home imports this file, and a value import back would be a cycle.
const KINDS: readonly TDesignKind[] = ['site', 'slides', 'video', 'ds'];

/** One choice of the filter menu: it stays open, so several can be set in a row. */
const Choice = ({ on, label, onPick }: { on: boolean; label: string; onPick: () => void }) => (
  <Menu.Item className="hx-cmenu__item" closeOnClick={false} aria-checked={on} onClick={onPick}>
    <span className="hx-cmenu__txt">{label}</span>
    <Icon name="check" size="sm" className="hx-cmenu__check" />
  </Menu.Item>
);

const Group = ({ label, children }: { label: string; children: ReactNode }) => (
  <Menu.Group>
    <Menu.GroupLabel className="hx-dl__mlabel">{label}</Menu.GroupLabel>
    {children}
  </Menu.Group>
);

export interface ILibraryBarProps {
  filters: ILibraryFilters;
  onFilters: (next: ILibraryFilters) => void;
  systems: { id: string; label: string }[];
  /** The person's own tools: absent where the library cannot be laid out (an older runtime). */
  tools: {
    selecting: boolean;
    onNewFolder: () => void;
    onSelect: () => void;
    onTrash: () => void;
    /** «Загрузить пакет»: absent where packs are not offered. */
    onImportPack?: () => void;
  } | null;
}

const LibraryBar = ({ filters, onFilters, systems, tools }: ILibraryBarProps) => {
  const filterActions = useMenuActions();
  const moreActions = useMenuActions();
  const changed = isNarrowed(filters) || filters.sort !== SORT.changed;
  return (
    <>
      <Menu.Root actionsRef={filterActions}>
        <Menu.Trigger
          className="hx-dl__ibtn hx-tipped"
          data-on={changed || undefined}
          aria-label={t('next.designly.filters')}
        >
          <Icon name="sliders-horizontal" size="sm" />
          <span className="hx-tip" data-pos="below">
            {t('next.designly.filters')}
          </span>
        </Menu.Trigger>
        <MenuPanel
          actions={filterActions}
          className="hx-cmenu hx-dl__fmenu"
          label={t('next.designly.filters')}
          side="bottom"
          align="end"
          sideOffset={6}
        >
          <Group label={t('next.designly.kindLabel')}>
            <Choice
              on={filters.kind === null}
              label={t('next.designly.kindAny')}
              onPick={() => onFilters({ ...filters, kind: null })}
            />
            {KINDS.map((k) => (
              <Choice
                key={k}
                on={filters.kind === k}
                label={t(`next.designly.kindFilter.${k}`)}
                onPick={() => onFilters({ ...filters, kind: k })}
              />
            ))}
            <Choice
              on={filters.recent}
              label={t('next.designly.recent')}
              onPick={() => onFilters({ ...filters, recent: !filters.recent })}
            />
          </Group>
          {systems.length > 0 && (
            <>
              <Menu.Separator className="hx-cmenu__sep" />
              <Group label={t('next.designly.system')}>
                <Choice
                  on={filters.system === null}
                  label={t('next.designly.systemAny')}
                  onPick={() => onFilters({ ...filters, system: null })}
                />
                {systems.map((s) => (
                  <Choice
                    key={s.id}
                    on={filters.system === s.id}
                    label={s.label}
                    onPick={() => onFilters({ ...filters, system: s.id })}
                  />
                ))}
              </Group>
            </>
          )}
          <Menu.Separator className="hx-cmenu__sep" />
          <Group label={t('next.designly.sortBy')}>
            {SORTS.map((s) => (
              <Choice
                key={s}
                on={filters.sort === s}
                label={t(`next.designly.sort.${s}`)}
                onPick={() => onFilters({ ...filters, sort: s })}
              />
            ))}
          </Group>
        </MenuPanel>
      </Menu.Root>
      {tools && (
        <Menu.Root actionsRef={moreActions}>
          <Menu.Trigger
            className="hx-dl__ibtn hx-tipped"
            data-on={tools.selecting || undefined}
            aria-label={t('next.designly.more')}
          >
            <Icon name="ellipsis" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.designly.more')}
            </span>
          </Menu.Trigger>
          <MenuPanel
            actions={moreActions}
            className="hx-cmenu"
            label={t('next.designly.more')}
            side="bottom"
            align="end"
            sideOffset={6}
          >
            <Menu.Item className="hx-cmenu__item" onClick={tools.onNewFolder}>
              <Icon name="folder-plus" size="sm" />
              <span className="hx-cmenu__txt">{t('next.designly.newFolder')}</span>
            </Menu.Item>
            <Menu.Item className="hx-cmenu__item" onClick={tools.onSelect}>
              <Icon name="list-checks" size="sm" />
              <span className="hx-cmenu__txt">
                {tools.selecting ? t('next.designly.selectStop') : t('next.designly.select')}
              </span>
            </Menu.Item>
            {tools.onImportPack && (
              <Menu.Item className="hx-cmenu__item" onClick={tools.onImportPack}>
                <Icon name="upload" size="sm" />
                <span className="hx-cmenu__txt">{t('next.pack.importPick')}</span>
              </Menu.Item>
            )}
            <Menu.Item className="hx-cmenu__item" onClick={tools.onTrash}>
              <Icon name="trash-2" size="sm" />
              <span className="hx-cmenu__txt">{t('next.designly.trash')}</span>
            </Menu.Item>
          </MenuPanel>
        </Menu.Root>
      )}
    </>
  );
};

export default LibraryBar;
