// A person by name or address: the field asks the gateway's people search (the one the documents use for «@») as
// the person types and offers the hits under it; ↑ ↓ walk them, Enter takes one. An address typed in full is taken as
// it is.
import { useEffect, useState, type RefObject } from 'react';
import { searchPeople, type IPersonHit } from '@/next/features/designly/access/api';
import { initialOf } from '@/next/features/designly/access/labels';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const ASK_MS = 200;
const OUT_MS = 160;

interface IPersonFieldProps {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
  /** Inside a frame of its own (the add field): no border of the input itself. */
  bare?: boolean;
  inputRef?: RefObject<HTMLInputElement | null>;
}

const PersonField = ({
  label,
  placeholder,
  value,
  onChange,
  disabled = false,
  bare = false,
  inputRef,
}: IPersonFieldProps) => {
  const [hits, setHits] = useState<IPersonHit[] | null>(null);
  const [active, setActive] = useState(-1);
  const query = value.trim();

  useEffect(() => {
    if (query.length < 2) return;
    const abort = new AbortController();
    const timer = setTimeout(() => {
      searchPeople(query, abort.signal).then(
        (found) => {
          setHits(found);
          setActive(-1);
        },
        () => undefined,
      );
    }, ASK_MS);
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  }, [query]);

  // A pick ends the list; so does a short query.
  const shown =
    query.length >= 2 && hits && hits.length > 0 && !hits.some((h) => h.id === query) ? hits : null;
  const list = usePresence(shown, OUT_MS);

  const pick = (hit: IPersonHit) => {
    setActive(-1);
    onChange(hit.id);
  };

  return (
    <div className="hx-acc-find">
      <input
        ref={inputRef}
        className={bare ? 'hx-acc-field__in' : 'hx-create__in hx-acc-in'}
        type="text"
        autoComplete="off"
        aria-label={label}
        aria-expanded={shown !== null}
        aria-autocomplete="list"
        role="combobox"
        placeholder={placeholder}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (!shown) return;
          if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
            event.preventDefault();
            const step = event.key === 'ArrowDown' ? 1 : -1;
            setActive((at) => (at + step + shown.length) % shown.length);
          } else if (event.key === 'Enter' && shown[active]) {
            event.preventDefault();
            pick(shown[active]);
          }
        }}
      />
      {list.value && (
        <div
          className="hx-acc-hits"
          data-state={list.state}
          role="listbox"
          aria-label={t('next.access.hits')}
        >
          {list.value.map((hit, index) => (
            <button
              key={hit.id}
              type="button"
              role="option"
              aria-selected={index === active}
              className="hx-option hx-acc-hit"
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => pick(hit)}
            >
              <span className="hx-acc-av" aria-hidden="true">
                {initialOf(hit.name || hit.id)}
              </span>
              <span className="hx-option__text">
                <span className="hx-option__name">{hit.name}</span>
                <span className="hx-option__desc">{hit.id}</span>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

export default PersonField;
