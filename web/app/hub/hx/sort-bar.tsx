// The search box and the «Сортировка» chips over a list, on the shared state of ./sort (the address keeps it). It is
// drawn only inside the lively hub (`ui.hx.live`): without the flag the lists look as they did.
import './styles/tools.css';
import { useParams, nextSort, readSort, type SortState } from './sort';
import { useLive } from './live';

export interface SortKey {
  id: string;
  label: string;
  /** The direction a first click starts with. */
  first?: 'asc' | 'desc';
}

/** The sort and the search of a screen as state of the address: `keys` are the ids a link may carry. */
export const useListView = (keys: readonly SortKey[], fallback: SortState) => {
  const p = useParams();
  const ids = keys.map((k) => k.id);
  const sort = readSort(p.search, ids, fallback);
  const q = p.get('q') ?? '';
  return {
    sort,
    q,
    setQ: (v: string) => p.set({ q: v }),
    pick: (id: string) => {
      const n = nextSort(sort, id, keys.find((k) => k.id === id)?.first ?? 'desc');
      p.set({ sort: n.key, dir: n.dir });
    },
    reset: () => p.set({ q: null, sort: null, dir: null }),
    touched: q !== '' || p.get('sort') !== null,
  };
};

export const ListBar = ({
  keys,
  view,
  placeholder,
}: {
  keys: readonly SortKey[];
  view: ReturnType<typeof useListView>;
  placeholder: string;
}) => {
  const live = useLive();
  if (!live) return null;
  return (
    <div className="live-bar">
      <input
        type="search"
        className="hx-set__input live-bar__q"
        placeholder={placeholder}
        aria-label="Поиск"
        value={view.q}
        onChange={(e) => view.setQ(e.target.value)}
      />
      <div className="hx-set__seg" role="group" aria-label="Сортировка">
        {keys.map((k) => {
          const on = view.sort.key === k.id;
          return (
            <button
              key={k.id}
              type="button"
              className="hx-set__seg-item"
              aria-pressed={on}
              aria-sort={on ? (view.sort.dir === 'asc' ? 'ascending' : 'descending') : undefined}
              onClick={() => view.pick(k.id)}
            >
              {k.label}
              {on ? (view.sort.dir === 'asc' ? ' ↑' : ' ↓') : ''}
            </button>
          );
        })}
      </div>
    </div>
  );
};

/** The note under a list that the search emptied, with the way back. */
export const NoMatch = ({ view }: { view: ReturnType<typeof useListView> }) => (
  <div className="tl-empty live-nomatch">
    <b>Ничего не нашлось</b>
    <span>Попробуйте другое слово или сбросьте поиск.</span>
    <button type="button" className="hx-set__btn" onClick={view.reset}>
      Сбросить поиск и вид
    </button>
  </div>
);
