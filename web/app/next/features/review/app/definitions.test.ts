import { describe, expect, it } from 'vitest';
import { findDefinitions, wordAt } from '@/next/features/review/app/definitions';

const go = `package a

type Store struct {
	items map[string]int
}

func (s *Store) Put(k string) {
	s.items[k] = 1
}

func NewStore() *Store {
	return &Store{}
}
`;

const ts = `export const placeNote = (note: Note) => {
  return note;
};

export class Layer {
  attach(slot: Slot): void {
    use(placeNote);
  }
  readonly subscribe = (fn: () => void) => fn;
}
`;

describe('findDefinitions', () => {
  it('finds Go types, functions and methods', () => {
    const files = [{ path: 'a.go', text: go }];
    expect(findDefinitions('Store', files).map((d) => d.line)).toEqual([3]);
    expect(findDefinitions('Put', files).map((d) => d.line)).toEqual([7]);
    expect(findDefinitions('NewStore', files).map((d) => d.text)).toEqual(['func NewStore() *Store {']);
  });
  it('finds TypeScript consts, classes, methods and fields, not their uses', () => {
    const files = [{ path: 'b.ts', text: ts }];
    expect(findDefinitions('placeNote', files).map((d) => d.line)).toEqual([1]);
    expect(findDefinitions('Layer', files).map((d) => d.line)).toEqual([5]);
    expect(findDefinitions('attach', files).map((d) => d.line)).toEqual([6]);
    expect(findDefinitions('subscribe', files).map((d) => d.line)).toEqual([9]);
  });
  it('puts the file asked from first and finds CSS variables', () => {
    const files = [
      { path: 'x.css', text: ':root {\n  --hx-gap: 4px;\n}\n.a { gap: var(--hx-gap); }' },
      { path: 'b.ts', text: ts },
      { path: 'c.ts', text: 'export function Layer() {}' },
    ];
    expect(findDefinitions('--hx-gap', files)).toEqual([{ path: 'x.css', line: 2, text: '--hx-gap: 4px;' }]);
    expect(findDefinitions('Layer', files, 'c.ts').map((d) => d.path)).toEqual(['c.ts', 'b.ts']);
    expect(findDefinitions('x', files)).toEqual([]);
  });
});

it('reads the word under a column', () => {
  expect(wordAt('  use(placeNote);', 8)).toBe('placeNote');
  expect(wordAt('  use(placeNote);', 1)).toBeNull();
  expect(wordAt('gap: var(--hx-gap);', 12)).toBe('--hx-gap');
});
