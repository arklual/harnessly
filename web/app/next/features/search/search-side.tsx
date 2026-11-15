// The side of the search (⌘K): what the selected row is. The name is the largest thing here; under it come the last
// words of a chat, a cover, facts and lists of what belongs to it. What can be done with it is in the menu of «⋯» (and
// of a right click on the row); the foot only reminds the keys.
import { Menu } from '@base-ui/react/menu';
import type { IDetailItem, IRowDetail } from '@/next/features/search/detail';
import { partsOf } from '@/next/features/search/highlight';
import type { ISearchRow } from '@/next/features/search/search-dialog';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import MenuPanel, { MenuRoot, useMenuActions } from '@/next/ui/menu-panel';
import { Skeleton } from '@/next/ui/skeleton';

export const SIDE_ACT = {
  widget: 'widget',
  open: 'open',
  aside: 'aside',
  chat: 'chat',
  copy: 'copy',
} as const;
export type TSideAct = (typeof SIDE_ACT)[keyof typeof SIDE_ACT];

export interface ISideAction {
  id: TSideAct;
  icon: string;
  label: string;
  /** The key that does it; '' for none. */
  key: string;
}

/** Where the menu of actions opens: from «⋯», or at the point a row was right-clicked. */
export interface ISideMenu {
  open: boolean;
  at: { x: number; y: number } | null;
}
export const SIDE_MENU_CLOSED: ISideMenu = { open: false, at: null };

interface ISearchSideProps {
  row: ISearchRow;
  /** What the row is, in a word. */
  kind: string;
  /** The words lit in the texts. */
  lit: string;
  /** null while the search has no source to ask. */
  detail: IRowDetail | null;
  actions: readonly ISideAction[];
  copied: boolean;
  /** A chat's stop or wake is on its way. */
  busy: boolean;
  menu: ISideMenu;
  onMenu: (menu: ISideMenu) => void;
  onAct: (id: TSideAct) => void;
  onItem: (item: IDetailItem) => void;
}

const Lit = ({ text, query }: { text: string; query: string }) => (
  <>
    {partsOf(text, query).map((part, i) => (
      <span key={i} data-hit={part.hit || undefined}>
        {part.text}
      </span>
    ))}
  </>
);

const pointAt = ({ x, y }: { x: number; y: number }) => ({
  getBoundingClientRect: () => new DOMRect(x, y, 0, 0),
});

const SearchSide = ({
  row,
  kind,
  lit,
  detail,
  actions,
  copied,
  busy,
  menu,
  onMenu,
  onAct,
  onItem,
}: ISearchSideProps) => {
  const menuActions = useMenuActions();
  const shown = detail?.detail ?? null;
  const loading = detail?.loading ?? false;
  const fallback = row.preview?.text || row.snippet || '';
  const text = shown?.text ?? (shown && shown.quotes.length > 0 ? '' : fallback);
  const keyed = actions.filter((action) => action.key !== '');
  const labelOf = (action: ISideAction) =>
    action.id === SIDE_ACT.copy && copied ? t('next.search.copied') : action.label;

  return (
    <aside className="hx-srch__side">
      <div className="hx-srch__side-body">
        <div className="hx-srch__side-head">
          <div className="hx-srch__side-name">
            <div className="hx-srch__side-kind">{kind}</div>
            <h3 className="hx-srch__side-title">{row.title}</h3>
          </div>
          {actions.length > 0 && (
            <MenuRoot
              actionsRef={menuActions}
              open={menu.open}
              onOpenChange={(open) => onMenu(open ? { open, at: menu.at } : SIDE_MENU_CLOSED)}
            >
              <Menu.Trigger
                render={
                  <button type="button" className="hx-srch__side-more" aria-label={t('next.search.more')}>
                    <Icon name="ellipsis" size="sm" />
                  </button>
                }
              />
              <MenuPanel
                actions={menuActions}
                className="hx-cmenu"
                label={row.title}
                align={menu.at ? 'start' : 'end'}
                menu="search-actions"
                {...(menu.at ? { anchor: pointAt(menu.at) } : {})}
              >
                {actions.map((action) => (
                  <Menu.Item
                    key={action.id}
                    className="hx-cmenu__item"
                    disabled={action.id === SIDE_ACT.chat && busy}
                    onClick={() => onAct(action.id)}
                  >
                    <Icon name={action.icon} size="sm" />
                    <span className="hx-cmenu__txt">{labelOf(action)}</span>
                    {action.key !== '' && <kbd className="hx-srch__menu-key">{action.key}</kbd>}
                  </Menu.Item>
                ))}
              </MenuPanel>
            </MenuRoot>
          )}
        </div>
        {shown?.cover && (
          <img className="hx-srch__cover" src={shown.cover.src} alt={shown.cover.alt} loading="lazy" />
        )}
        {shown?.quotes.map((quote, i) => (
          <div key={i} className="hx-srch__quote">
            <div className="hx-srch__quote-who">
              <span>{quote.who}</span>
              {quote.when && <span>{quote.when}</span>}
            </div>
            <div className="hx-srch__quote-text">
              <Lit text={quote.text} query={lit} />
            </div>
          </div>
        ))}
        {text !== '' && (
          <div className="hx-srch__side-text">
            <Lit text={text} query={lit} />
          </div>
        )}
        {loading && (
          <div className="hx-srch__side-sk" role="status" aria-label={t('next.search.detailLoading')}>
            <Skeleton className="hx-srch__sk-line" />
            <Skeleton className="hx-srch__sk-line" />
            <Skeleton className="hx-srch__sk-line hx-srch__sk-line--short" />
          </div>
        )}
        {shown && shown.facts.length > 0 && (
          <dl className="hx-srch__facts">
            {shown.facts.map((fact) => (
              <div key={fact.label} className="hx-srch__fact">
                <dt>{fact.label}</dt>
                <dd title={fact.title}>{fact.value}</dd>
              </div>
            ))}
          </dl>
        )}
        {shown?.lists.map((list) => (
          <div key={list.label} className="hx-srch__sublist">
            <div className="hx-srch__side-kind">{list.label}</div>
            {list.items.map((item) => (
              <button
                key={item.id}
                type="button"
                className="hx-srch__subitem"
                disabled={!item.go}
                title={item.title}
                onClick={() => onItem(item)}
              >
                <Icon name={item.icon} size="sm" />
                <span>{item.title}</span>
                {item.meta && <small>{item.meta}</small>}
              </button>
            ))}
          </div>
        ))}
      </div>
      {keyed.length > 0 && (
        <div className="hx-srch__side-keys">
          {keyed.map((action) => (
            <span key={action.id}>
              <kbd>{action.key}</kbd>
              {labelOf(action)}
            </span>
          ))}
        </div>
      )}
    </aside>
  );
};

export default SearchSide;
