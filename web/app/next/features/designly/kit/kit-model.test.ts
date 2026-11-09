import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { shelfCards, type IIndexEntry, type IKitCanvas } from '@/next/features/designly/kit/kit-api';
import { callOf, findNode, NODE } from '@/next/features/designly/kit/kit-node';
import { groupResults, searchIndex } from '@/next/features/designly/kit/kit-search';
import { pathTo, treeOf, visibleRows } from '@/next/features/designly/kit/kit-tree';
import {
  buildWorld,
  itemAt,
  MAIN_VIEW,
  rectOf,
  VIEW,
  viewOfNode,
} from '@/next/features/designly/kit/kit-world';
import { boardNode } from '@/next/features/designly/kit/use-kit-link';

// An invented kit: one component, two icons, a template of two sections.
const canvas: IKitCanvas = {
  kit: 'acme-cards',
  version: '1.2.0',
  versions: ['1.2.0'],
  title: 'Acme Cards',
  theme: 'sun',
  themes: [{ id: 'sun', title: 'Sun' }],
  namespace: 'Acme',
  base: 'sun',
  sections: [
    { id: 'foundation', title: '' },
    { id: 'components', title: '' },
    { id: 'templates', title: '' },
    { id: 'reference', title: '' },
    { id: 'assets', title: '' },
  ],
  modes: null,
  colors: [{ id: 'sky', name: 'sky', values: { '': '#3388ff' } }],
  type: null,
  fonts: null,
  tokens: null,
  groups: [
    {
      id: 'basics',
      title: 'Basics',
      components: [
        {
          id: 'Badge',
          node: 'component:Badge',
          name: 'Badge',
          props: [{ name: 'text', type: 'string', required: true }],
          variants: [],
          sample: { text: 'New' },
          cell: -1,
        },
      ],
    },
  ],
  assets: [
    {
      id: 'icons',
      node: 'assets:icons',
      title: 'Icons',
      kind: 'icon',
      component: 'Icon',
      prop: 'name',
      items: [
        { node: 'asset:icons/star', name: 'star', file: 'icons/star.png', cell: 0 },
        { node: 'asset:icons/moon', name: 'moon', file: 'icons/moon.png', cell: 1 },
      ],
    },
  ],
  templates: [
    {
      id: 'deck',
      node: 'template:deck',
      title: 'Deck',
      kind: 'slides',
      aspect: [16, 9],
      component: 'Slide',
      sections: [
        {
          id: 'covers',
          node: 'tsection:deck/covers',
          title: 'Covers',
          layouts: [
            {
              id: 'cover',
              node: 'layout:deck/cover',
              name: 'Cover',
              props: { layout: 'c1' },
              slots: [],
              cell: 0,
            },
          ],
        },
        {
          id: 'content',
          node: 'tsection:deck/content',
          title: 'Content',
          layouts: [{ id: 'list', node: 'layout:deck/list', name: 'List of points', slots: [], cell: 1 }],
        },
      ],
    },
    {
      id: 'guide',
      node: 'template:guide',
      section: 'reference',
      title: 'How to use',
      kind: 'slides',
      aspect: [16, 9],
      sections: [
        {
          id: 'how',
          node: 'tsection:guide/how',
          title: 'How',
          layouts: [{ id: 'how1', node: 'layout:guide/how1', name: 'Guides', slots: [], cell: -1 }],
        },
      ],
    },
  ],
  index: [
    { node: 'component:Badge', kind: 'component', title: 'Badge', path: 'Basics', words: 'badge label' },
    { node: 'asset:icons/star', kind: 'asset', title: 'star', path: 'Icons', words: 'star' },
    {
      node: 'layout:deck/list',
      kind: 'layout',
      title: 'List of points',
      path: 'Deck › Content',
      words: 'list points',
    },
  ],
  preparedAt: 1,
};

const titles = {
  foundation: 'Foundation',
  components: 'Components',
  templates: 'Templates',
  assets: 'Assets',
  reference: 'Guide',
  colors: 'Colours',
  type: 'Typography',
  tokens: 'Tokens',
};

describe('the kit canvas model', () => {
  it('finds nodes by id and writes the call of a component and an icon', () => {
    const badge = findNode(canvas, 'component:Badge');
    expect(badge?.kind).toBe(NODE.component);
    expect(badge && callOf(canvas, badge, { text: 'Hi' })).toContain('Acme.Badge');
    const star = findNode(canvas, 'asset:icons/star');
    expect(star && callOf(canvas, star)).toContain('star');
    expect(findNode(canvas, 'asset:icons/none')).toBeNull();
  });

  it('searches the index and groups the hits by kind', () => {
    expect(searchIndex(canvas.index ?? [], 'points')[0]?.node).toBe('layout:deck/list');
    expect(searchIndex(canvas.index ?? [], 'zzz')).toEqual([]);
    const groups = groupResults(searchIndex(canvas.index ?? [], 'a'));
    expect(new Set(groups.map((g) => g.kind)).size).toBe(groups.length);
  });

  it('lays every node out on the main canvas and a template on its own view', () => {
    const main = buildWorld(canvas, MAIN_VIEW, titles, null);
    const star = rectOf(main, 'asset:icons/star');
    expect(star).not.toBeNull();
    if (star) expect(itemAt(main, star.x + star.w / 2, star.y + star.h / 2)?.node).toBe('asset:icons/star');
    expect(viewOfNode('layout:deck/list')).toEqual({ kind: VIEW.template, id: 'deck' });
    const deck = buildWorld(canvas, { kind: VIEW.template, id: 'deck' }, titles, null);
    expect(rectOf(deck, 'layout:deck/list')).not.toBeNull();
  });

  it("keeps a template's guide in its own section, apart from the templates", () => {
    const tree = treeOf(canvas, titles);
    const templates = tree.find((n) => n.node === 'section:templates');
    const guide = tree.find((n) => n.node === 'section:reference');
    expect(templates?.children?.map((c) => c.node)).toEqual(['template:deck']);
    expect(guide?.children?.map((c) => c.node)).toEqual(['template:guide']);
    const main = buildWorld(canvas, MAIN_VIEW, titles, null);
    expect(rectOf(main, 'template:guide')).not.toBeNull();
  });

  it('shows a kit of several themes on the shelf as a card per theme', () => {
    const base = { id: 'acme-cards', title: 'Acme Cards', version: '1.2.0', versions: ['1.2.0'], themes: [] };
    const one = shelfCards([{ ...base, cover: 'sun/c.jpg', coverTheme: 'sun', layouts: 3, assets: 2 }]);
    expect(one.map((c) => [c.title, c.theme])).toEqual([['Acme Cards', null]]);
    const many = shelfCards([
      {
        ...base,
        cover: '',
        coverTheme: 'sun',
        layouts: 5,
        assets: 0,
        themeCards: [
          { id: 'sun', title: 'Sun', cover: 'sun/c.jpg', layouts: 3 },
          { id: 'moon', title: 'Moon', cover: 'moon/c.jpg', layouts: 2 },
        ],
      },
    ]);
    expect(many.map((c) => [c.title, c.theme, c.kitTitle, c.layouts])).toEqual([
      ['Sun', 'sun', 'Acme Cards', 3],
      ['Moon', 'moon', 'Acme Cards', 2],
    ]);
  });

  it('builds a tree that opens down to a picked node', () => {
    const tree = treeOf(canvas, titles);
    const path = pathTo(tree, 'asset:icons/moon');
    expect(path).not.toBeNull();
    const rows = visibleRows(tree, new Set(path ?? []));
    expect(rows.some((r) => r.node === 'asset:icons/moon')).toBe(true);
  });

  it('reads the kit node a board shows from the project link', () => {
    const link = {
      project: 'p1',
      title: 'Talk',
      kit: 'acme-cards',
      kitTitle: 'Acme Cards',
      theme: 'sun',
      version: '1.2.0',
      latest: '1.2.0',
      boards: [{ path: 'slides/01.dc.html', title: '1', nodes: ['layout:deck/cover'] }],
      nodes: { 'layout:deck/cover': { name: 'Cover' } },
    };
    expect(boardNode(link, 'slides/01.dc.html')).toBe('layout:deck/cover');
    expect(boardNode(link, 'slides/02.dc.html')).toBeNull();
    expect(boardNode(null, 'slides/01.dc.html')).toBeNull();
  });
});

describe('the kit search groups', () => {
  it('puts the kind of the best hit first', () => {
    const groups = groupResults(searchIndex(canvas.index ?? [], 'star'));
    expect(groups[0]?.kind).toBe('asset');
  });
});

// The same cases the agent's search runs (internal/designkit/index_test.go): the two searches never drift apart.
describe('kit search cases shared with the runtime', () => {
  const raw = readFileSync(
    fileURLToPath(
      new URL('../../../../../../internal/designkit/testdata/search-cases.json', import.meta.url),
    ),
    'utf8',
  );
  const file = JSON.parse(raw) as { index: IIndexEntry[]; cases: { q: string; want: string[] }[] };
  for (const c of file.cases)
    it(`finds «${c.q}»`, () => {
      const got = searchIndex(file.index, c.q, 0).map((e) => e.node);
      if (c.want.length === 0) expect(got).toEqual([]);
      else expect(got.slice(0, c.want.length)).toEqual(c.want);
    });
});
