// @vitest-environment jsdom
// Sergey picks a row in ⌘K and a later answer (the messages, the index's designs, a source) comes in above it:
// the list moved under him and Enter opened another chat. The rows already shown stay; the selection is held by id.
import { createRef } from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import SearchDialog, {
  steadyGroups,
  type ISearchGroup,
  type ISearchRow,
} from '@/next/features/search/search-dialog';

const row = (id: string, title: string): ISearchRow => ({ id, kind: 'chat', icon: 'chat', title, meta: '' });
const chats: ISearchGroup = {
  id: 'chats',
  label: 'Чаты',
  rows: [row('tab-a', 'Альфа'), row('tab-b', 'Бета')],
};
const messages: ISearchGroup = {
  id: 'messages',
  label: 'Сообщения',
  rows: [row('m-1', 'Сообщение один'), row('m-2', 'Сообщение два')],
};

// The dialog mounts into the interface's own root, as in the app.
const host = createRef<HTMLDivElement>();
const dialog = (results: readonly ISearchGroup[], onPick: (row: ISearchRow) => void, query = 'а') => (
  <PortalContext.Provider value={host}>
    <div className="hx-root" ref={host} />
    <SearchDialog
      open
      onClose={() => {}}
      query={query}
      onQuery={() => {}}
      scopes={[]}
      scope=""
      onScope={() => {}}
      results={results}
      recent={[]}
      onPick={onPick}
    />
  </PortalContext.Provider>
);

const titles = () => screen.getAllByRole('option').map((option) => option.textContent ?? '');

// jsdom has no layout: the arrows scroll the selected row into view.
Element.prototype.scrollIntoView = () => {};

afterEach(cleanup);

describe('⌘K holds what it shows (HRN-1446)', () => {
  it('a later answer above the selected row does not change what Enter opens', () => {
    const onPick = vi.fn();
    const { rerender } = render(dialog([chats], onPick));
    const input = screen.getByRole('searchbox');
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    // The messages come in and would stand first.
    rerender(dialog([messages, chats], onPick));
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick.mock.calls[0]?.[0]).toMatchObject({ id: 'tab-b' });
  });

  it('the rows shown stay where they were, the new ones come below', () => {
    const { rerender } = render(dialog([chats], vi.fn()));
    rerender(dialog([messages, { ...chats, rows: [row('tab-c', 'Гамма'), ...chats.rows] }], vi.fn()));
    expect(titles().map((text) => text.slice(0, 5))).toEqual(['Альфа', 'Бета', 'Гамма', 'Сообщ', 'Сообщ']);
  });

  it('the selection stays on its row when it moves', () => {
    const onPick = vi.fn();
    const { rerender } = render(dialog([chats], onPick));
    const input = screen.getByRole('searchbox');
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    // The live list re-ranks the group: a new chat first, the old first one goes.
    rerender(
      dialog(
        [{ ...chats, rows: [row('tab-c', 'Гамма'), row('tab-d', 'Дельта'), row('tab-b', 'Бета')] }],
        onPick,
      ),
    );
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onPick.mock.calls[0]?.[0]).toMatchObject({ id: 'tab-b' });
  });

  it('new words start a new order', () => {
    const { rerender } = render(dialog([chats], vi.fn()));
    rerender(dialog([messages, chats], vi.fn(), 'аб'));
    expect(titles()[0]?.startsWith('Сообщение один')).toBe(true);
  });
});

describe('steadyGroups', () => {
  it('keeps the groups and rows of the layout first', () => {
    const layout = new Map([['chats', ['tab-b', 'tab-a']]]);
    const out = steadyGroups([messages, chats], layout);
    expect(out.map((g) => g.id)).toEqual(['chats', 'messages']);
    expect(out[0]?.rows.map((r) => r.id)).toEqual(['tab-b', 'tab-a']);
  });
});
