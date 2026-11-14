// @vitest-environment jsdom
// the preview of a design in ⌘K said «Работали: Тимур, tab-k1yksasiw, tab-5sxwrqvwh, …» — the ids of the
// chats that edited it were read as people. People are names only, at most three and «и ещё N»; the chats are listed
// by their titles.
import { describe, expect, it } from 'vitest';
import { detailSource, peopleLine } from '@/next/bindings/search-detail';
import { SUBJECT } from '@/next/features/search/subject';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import { t } from '@/next/i18n';
import type { DesignArtifact, MachineId } from '@/next/protocol/v2';

const chat = (id: string, title: string): ISidebarChat => ({
  id,
  title,
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: 1,
  lastPromptAt: 1,
  lastActivityAt: 1,
  terminal: true,
  canStopTerminal: true,
});

const design: DesignArtifact = {
  id: 'site-1',
  title: 'Библиотека макетов',
  updatedAt: 1,
  kind: 'site',
  pinned: false,
  cover: null,
  projectPath: '',
  boards: [],
  convs: [],
  touchedBy: ['tab-k1yksasiw', 'tab-5sxwrqvwh', 'tab-_Rw62b'],
  owner: { name: 'Тимур', me: false },
  // What the runtime says: the owner and the people of the chats that edited it.
  people: ['Тимур', 'Глеб', 'Вера', 'Анна'],
};

describe('«Работали» in the preview of a design (HRN-1447)', () => {
  it('names people only and lists the chats that edited it by their titles', () => {
    const source = detailSource({
      machine: 'm1' as MachineId,
      chats: [chat('tab-k1yksasiw', 'Шапка сайта')],
      now: 2,
    });
    const detail = source.now({
      id: 'design:site-1',
      kind: 'design',
      icon: 'pen-tool',
      title: design.title,
      meta: '',
      subject: { k: SUBJECT.design, design, board: null },
    });
    const people = detail.facts.find((fact) => fact.label === t('next.search.factPeople'));
    expect(people?.value).toBe(t('next.search.peopleMore', { names: 'Тимур, Глеб, Вера', n: 1 }));
    expect(JSON.stringify(detail.facts)).not.toContain('tab-');
    expect(detail.lists.flatMap((list) => list.items.map((item) => item.title))).toEqual(['Шапка сайта']);
  });

  it('shows three names and «и ещё N», each once, never an id', () => {
    expect(peopleLine(['Тимур', 'Глеб', 'Тимур', 'tab-x1', 'Вера', 'Анна', 'Борис'])).toBe(
      t('next.search.peopleMore', { names: 'Тимур, Глеб, Вера', n: 2 }),
    );
    expect(peopleLine(['Тимур', 'timur@team.test'])).toBe('Тимур');
  });
});
