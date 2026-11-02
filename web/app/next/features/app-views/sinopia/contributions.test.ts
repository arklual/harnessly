import { describe, expect, it } from 'vitest';
import {
  activeApps,
  boardAt,
  inspectorFor,
  marksOfBoard,
  panelsOf,
  parseNodeId,
  readHlClick,
  readHlHello,
  selectedOf,
  selectionActions,
} from '@/next/features/app-views/sinopia/contributions';
import { boards, cashback, lint, SLOT } from '@/next/features/app-views/sinopia/fixtures';

const all = [lint, cashback];

describe('activeApps: «Валидатор презентаций» is on in slides only', () => {
  it('shows its panel in a presentation', () => {
    const active = activeApps(all, [lint.id], 'slides');
    expect(active.map((a) => a.app.id)).toEqual([lint.id]);
    expect(panelsOf(active).map((p) => p.key)).toEqual(['harnessly.deck-lint/issues']);
  });

  it('shows nothing of it in a site, a video or a design system', () => {
    for (const kind of ['site', 'video', 'ds']) {
      expect(activeApps(all, [lint.id], kind), kind).toEqual([]);
    }
  });

  it('shows no app the runtime did not say is on in the document', () => {
    expect(activeApps(all, [], 'slides')).toEqual([]);
    expect(activeApps(all, [cashback.id], 'slides')).toEqual([]);
  });

  it('keeps a panel to the kinds it names', () => {
    const narrow = {
      ...lint,
      targets: ['sinopia.*'],
      views: [{ id: 'panel', targets: ['sinopia.*'] }],
      sinopia: { panels: [{ id: 'p', view: 'panel', kinds: ['slides'] }] },
    };
    expect(panelsOf(activeApps([narrow], [narrow.id], 'slides'))).toHaveLength(1);
    expect(panelsOf(activeApps([narrow], [narrow.id], 'site'))).toHaveLength(0);
  });

  it('drops a panel whose view is not made for the document', () => {
    const odd = { ...lint, sinopia: { panels: [{ id: 'p', view: 'card' }] } };
    expect(panelsOf(activeApps([odd], [odd.id], 'slides'))).toEqual([]);
  });

  it('lists the selection items of the apps on in the document', () => {
    const actions = selectionActions(activeApps(all, [cashback.id], 'site'));
    expect(actions.map((a) => a.key)).toEqual(['harnessly.cashback/explain']);
  });
});

describe('inspectorFor: which view a bound element opens', () => {
  it('is the inspector of the app with bindings', () => {
    const got = inspectorFor(activeApps(all, [cashback.id], 'site'), { field: 'amount' });
    expect(got?.app.id).toBe(cashback.id);
    expect(got?.view).toBe('inspector');
  });

  it('is nothing where no app binds', () => {
    expect(inspectorFor(activeApps(all, [lint.id], 'slides'), { field: 'x' })).toBeNull();
  });

  it('follows data-hl-app when two apps bind', () => {
    const second = { ...cashback, id: 'acme.second', name: 'Второй' };
    const active = activeApps([cashback, second], [cashback.id, second.id], 'site');
    expect(inspectorFor(active, { field: 'a', app: 'acme.second' })?.app.id).toBe('acme.second');
    expect(inspectorFor(active, { field: 'a' })?.app.id).toBe(cashback.id);
  });
});

describe('the board of an address and the marks on it', () => {
  it('finds the board by the path after /p/', () => {
    expect(boardAt(boards, `${SLOT}/a/proj/p/index.dc.html`)?.id).toBe(boards[0].id);
    expect(boardAt(boards, `${SLOT}/a/proj/p/cashback/card.dc.html?x=1`)?.id).toBe(boards[1].id);
    expect(boardAt(boards, `${SLOT}/a/proj/p/other.html`)).toBeNull();
    expect(boardAt(boards, 'not a url')).toBeNull();
  });

  it('reads a node id', () => {
    expect(parseNodeId('01HQ:12')).toEqual({ page: '01HQ', tpl: 12 });
    for (const bad of ['01HQ', ':3', '01HQ:x', '01HQ:-1', '01HQ:1.5'])
      expect(parseNodeId(bad), bad).toBeNull();
  });

  it('gives a board its own marks, one per element, the worst level first', () => {
    const page = boards[0].id;
    const marks = marksOfBoard(
      [
        { node: `${page}:3`, level: 'warning', note: 'мелко', source: 'app:a' },
        { node: `${page}:3`, level: 'error', note: 'много слов', source: 'app:a' },
        { node: `${page}:3`, level: 'error', note: 'много слов', source: 'app:b' },
        { node: `${boards[1].id}:3`, level: 'error', note: 'чужая', source: 'app:a' },
        { node: 'broken', level: 'error', note: 'x', source: 'app:a' },
      ],
      page,
    );
    expect(marks).toEqual([{ tpl: 3, level: 'error', note: 'мелко\nмного слов' }]);
  });
});

describe('what a board says', () => {
  const click = {
    type: 'tk:hl-click',
    field: 'amount',
    hl: { field: 'amount', attr: 'x'.repeat(500), 'bad name': 'y' },
    tpl: 4,
    fp: 'abc',
    selector: '#amount',
    tag: 'span',
    text: '5%',
    rect: { x: 1, y: 2, width: 30, height: 10 },
    url: `${SLOT}/a/proj/p/cashback/card.dc.html`,
  };

  it('reads a click and clips the board’s words', () => {
    const got = readHlClick(click);
    expect(got?.field).toBe('amount');
    expect(got?.hl.attr).toHaveLength(200);
    expect(got?.hl).not.toHaveProperty('bad name');
  });

  it('refuses what is not a click', () => {
    expect(readHlClick(null)).toBeNull();
    expect(readHlClick({ ...click, type: 'tk:picked' })).toBeNull();
    expect(readHlClick({ ...click, field: '' })).toBeNull();
    expect(readHlClick({ ...click, rect: { x: 1 } })).toBeNull();
    expect(readHlHello({ type: 'tk:hl-hello', url: 'https://x' })).toBe('https://x');
    expect(readHlHello({ type: 'tk:hl-hello' })).toBeNull();
  });

  it('names the picked element by the board and the number', () => {
    const got = readHlClick(click);
    if (got === null) throw new Error('not read');
    const node = selectedOf(got, boards[1]);
    expect(node.id).toBe(`${boards[1].id}:4`);
    expect(node.field).toBe('amount');
    expect(node.page).toBe(boards[1].id);
  });
});
