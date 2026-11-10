// «Добавить людей или команды» (HRN-675): the field asks the directory as the person types and offers people and
// teams under it; a pick becomes a chip, Backspace in the empty field takes the last chip back, an address typed in
// full is offered as «Пригласить <почта>». With chips, one role for all of them and «Пригласить» come in at the end:
// each chip is given that role in turn, and the field empties once the gateway has answered.
import { useEffect, useMemo, useState, type RefObject } from 'react';
import { EMAIL } from '@/next/features/designly/access/api';
import { initialOf } from '@/next/features/designly/access/labels';
import {
  putGrant,
  searchDirectory,
  type IDirectory,
  type IGrant,
  type IResourceRef,
  type IResourceView,
  type TShareRole,
} from '@/next/features/designly/access/resource-api';
import RoleMenu from '@/next/features/designly/access/role-menu';
import type { IAction } from '@/next/features/designly/access/use-action';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';

const ASK_MS = 200;
const OUT_MS = 160;
const ROLES = ['view', 'comment', 'edit'] as const satisfies readonly TShareRole[];

export interface IChip {
  type: IGrant['type'];
  id: string;
  name: string;
  sub: string;
}

interface IResourceAddProps {
  target: IResourceRef;
  view: IResourceView;
  action: IAction;
  onNext: (next: IResourceView) => void;
  inputRef?: RefObject<HTMLInputElement | null>;
}

/** The mark of a row: a round initial for a person, a rounded square with people for a team. */
export const Mark = ({ type, name }: { type: IGrant['type']; name: string }) =>
  type === 'team' ? (
    <span className="hx-acc-av hx-acc-av--team" data-tone="team" aria-hidden="true">
      <Icon name="users" size="sm" />
    </span>
  ) : (
    <span className="hx-acc-av" aria-hidden="true">
      {initialOf(name)}
    </span>
  );

const hitsOf = (found: IDirectory | null, taken: Set<string>, query: string): IChip[] => {
  const list: IChip[] = [];
  for (const team of found?.teams ?? []) {
    const key = `team:${team.teamId}`;
    if (!taken.has(key))
      list.push({
        type: 'team',
        id: team.teamId,
        name: t('next.access.teamName', { name: team.name }),
        sub: t('next.access.teamSub', { org: team.orgName, n: team.members }),
      });
  }
  for (const person of found?.people ?? []) {
    if (!taken.has(`user:${person.email}`))
      list.push({ type: 'user', id: person.email, name: person.name, sub: person.email });
  }
  const email = query.toLowerCase();
  if (EMAIL.test(email) && !taken.has(`user:${email}`) && !list.some((hit) => hit.id === email))
    list.push({ type: 'user', id: email, name: t('next.access.inviteEmail', { email }), sub: '' });
  return list;
};

const ResourceAdd = ({ target, view, action, onNext, inputRef }: IResourceAddProps) => {
  const [query, setQuery] = useState('');
  const [chips, setChips] = useState<IChip[]>([]);
  const [role, setRole] = useState<TShareRole>('comment');
  const [found, setFound] = useState<IDirectory | null>(null);
  const [active, setActive] = useState(-1);
  const idle = action.busy === null;
  const q = query.trim();

  useEffect(() => {
    if (q.length < 2) return;
    const abort = new AbortController();
    const timer = setTimeout(() => {
      searchDirectory(q, abort.signal).then(
        (next) => {
          setFound(next);
          setActive(-1);
        },
        () => undefined,
      );
    }, ASK_MS);
    return () => {
      clearTimeout(timer);
      abort.abort();
    };
  }, [q]);

  // Who already has a place (or is a chip) is not offered again; nor is the owner.
  const taken = useMemo(() => {
    const keys = new Set<string>([
      ...(view.grants ?? []).map((g) => `${g.type}:${g.id}`),
      ...chips.map((c) => `${c.type}:${c.id}`),
    ]);
    if (view.owner?.email) keys.add(`user:${view.owner.email}`);
    if (view.homeTeam) keys.add(`team:${view.homeTeam.teamId}`);
    return keys;
  }, [view.grants, view.owner, view.homeTeam, chips]);

  // One array per answer: the list's presence follows its identity.
  const hits = useMemo(() => (q.length >= 2 ? hitsOf(found, taken, q) : []), [found, taken, q]);
  const shown = hits.length > 0 ? hits : null;
  const list = usePresence(shown, OUT_MS);
  const send = usePresence(chips.length > 0 ? true : null, OUT_MS);

  const take = (chip: IChip) => {
    setChips((now) => [...now, chip.sub === '' ? { ...chip, name: chip.id } : chip]);
    setQuery('');
    setFound(null);
    setActive(-1);
  };

  const invite = () =>
    void action.run('invite', async () => {
      const left = [...chips];
      let last: IResourceView | null = null;
      try {
        while (left.length > 0) {
          const chip = left[0];
          last = await putGrant(target, chip.type, chip.id, { role, canShare: false, ttl: null });
          left.shift();
        }
      } finally {
        // What the gateway took leaves the field; what it refused stays, with its words under the field.
        setChips(left);
        if (last) onNext(last);
      }
    });

  return (
    <form
      className="hx-acc-add"
      data-section="add"
      onSubmit={(event) => {
        event.preventDefault();
        if (shown && shown[Math.max(active, 0)] && q !== '') take(shown[Math.max(active, 0)]);
        else if (chips.length > 0) invite();
      }}
    >
      <div className="hx-acc-field hx-acc-field--chips">
        {chips.map((chip) => (
          <span key={`${chip.type}:${chip.id}`} className="hx-acc-chip">
            <Mark type={chip.type} name={chip.name} />
            <span className="hx-acc-chip__nm">{chip.name}</span>
            <button
              type="button"
              className="hx-acc-chip__x"
              aria-label={t('next.access.chipRemove', { who: chip.name })}
              disabled={!idle}
              onClick={() => setChips((now) => now.filter((c) => c !== chip))}
            >
              <Icon name="x" size="sm" />
            </button>
          </span>
        ))}
        <div className="hx-acc-find hx-acc-find--chips">
          <input
            ref={inputRef}
            className="hx-acc-field__in"
            type="text"
            autoComplete="off"
            role="combobox"
            aria-label={t('next.access.addWho')}
            aria-expanded={shown !== null}
            aria-autocomplete="list"
            placeholder={chips.length > 0 ? '' : t('next.access.addWho')}
            value={query}
            disabled={!idle}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Backspace' && query === '' && chips.length > 0) {
                event.preventDefault();
                setChips((now) => now.slice(0, -1));
                return;
              }
              if (!shown) return;
              if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                event.preventDefault();
                const step = event.key === 'ArrowDown' ? 1 : -1;
                setActive((now) => (now + step + shown.length) % shown.length);
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
                  key={`${hit.type}:${hit.id}`}
                  type="button"
                  role="option"
                  aria-selected={index === active}
                  className="hx-option hx-acc-hit"
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => take(hit)}
                >
                  {hit.sub === '' ? (
                    <span className="hx-acc-av" aria-hidden="true">
                      <Icon name="mail" size="sm" />
                    </span>
                  ) : (
                    <Mark type={hit.type} name={hit.name} />
                  )}
                  <span className="hx-option__text">
                    <span className="hx-option__name">{hit.name}</span>
                    {hit.sub && <span className="hx-option__desc">{hit.sub}</span>}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
        {send.value && (
          <span className="hx-acc-field__end" data-state={send.state}>
            <RoleMenu
              label={t('next.access.newRole')}
              role={role}
              roles={ROLES}
              onRole={setRole}
              disabled={!idle}
            />
          </span>
        )}
      </div>
      {send.value && (
        <button
          type="submit"
          className="hx-btn hx-acc-add__go"
          data-variant="primary"
          data-state={send.state}
          data-busy={action.busy === 'invite'}
          disabled={!idle}
        >
          <span>{t('next.access.invite')}</span>
          <span className="hx-gate__spin" aria-hidden="true" />
        </button>
      )}
      {q.length >= 2 && found !== null && hits.length === 0 && (
        <p className="hx-acc-note hx-acc-add__none">{t('next.access.nobody')}</p>
      )}
    </form>
  );
};

export default ResourceAdd;
