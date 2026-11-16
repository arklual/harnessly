import { describe, expect, it } from 'vitest';
import { chatOwner, newsFor, ownersOf, peopleView, splitForPeople } from '@/next/features/people/people';

const people = ['Артём', 'Маша'];
const artem = peopleView('Артём', people)!;
const labels = { shared: 'Общие', mine: 'Мои' };

const projects = [
  { id: 'p-site', name: 'сайт' },
  { id: 'p-diploma', name: 'диплом', owner: 'Маша' },
  { id: 'p-bot', name: 'бот', owner: 'Артём' },
  { id: 'p-lab', name: 'лаба', owner: 'Маша' },
];
const chat = (id: string, projectId: string | null, startedBy?: string) => ({
  id,
  projectId,
  ...(startedBy ? { startedBy } : {}),
});
const chats = [
  chat('c-site', 'p-site', 'Маша'),
  chat('c-diploma', 'p-diploma', 'Маша'),
  chat('c-bot', 'p-bot'),
  chat('c-loose-masha', null, 'Маша'),
  chat('c-loose-old', null),
  chat('c-gone-project', 'p-gone', 'Маша'),
];

describe('people', () => {
  it('is off until this browser names someone the machine lists', () => {
    expect(peopleView(null, people)).toBeNull();
    expect(peopleView('Петя', people)).toBeNull();
    expect(peopleView('Маша', people)).toEqual({ me: 'Маша', people });
  });

  it('a chat is its project’s, else its starter’s', () => {
    const owners = ownersOf(projects);
    expect(chatOwner(chat('a', 'p-site', 'Маша'), owners)).toBe('');
    expect(chatOwner(chat('b', 'p-diploma'), owners)).toBe('Маша');
    expect(chatOwner(chat('c', null, 'Маша'), owners)).toBe('Маша');
    expect(chatOwner(chat('d', 'p-gone', 'Маша'), owners)).toBe('Маша');
    expect(chatOwner(chat('e', null), owners)).toBe('');
  });

  it('news is for the one who wrote last, else for whoever’s chat it is', () => {
    expect(newsFor('Маша', '', artem)).toBe(false);
    expect(newsFor('Артём', 'Маша', artem)).toBe(true);
    expect(newsFor(undefined, '', artem)).toBe(true);
    expect(newsFor(undefined, 'Артём', artem)).toBe(true);
    expect(newsFor(undefined, 'Маша', artem)).toBe(false);
  });

  it('everyone’s first, then one’s own, another person’s folded under their name', () => {
    const split = splitForPeople(chats, projects, artem, new Set(), labels);
    expect(split.projects.map((p) => [p.id, p.band])).toEqual([
      ['p-site', 'Общие'],
      ['p-bot', 'Мои'],
    ]);
    expect(split.chats.map((c) => c.id)).toEqual(['c-site', 'c-bot', 'c-loose-old']);
    expect(split.folds).toEqual([{ person: 'Маша', projects: 2, chats: 2, open: false }]);
  });

  it('an unfolded person’s projects come last under their name; the chat on screen always stays', () => {
    const split = splitForPeople(chats, projects, artem, new Set(['Маша']), labels);
    expect(split.projects.map((p) => [p.id, p.band])).toEqual([
      ['p-site', 'Общие'],
      ['p-bot', 'Мои'],
      ['p-diploma', 'Маша'],
      ['p-lab', 'Маша'],
    ]);
    expect(split.chats).toHaveLength(chats.length);
    expect(split.folds[0]?.open).toBe(true);
    const kept = splitForPeople(chats, projects, artem, new Set(), labels, 'c-diploma');
    expect(kept.chats.map((c) => c.id)).toContain('c-diploma');
  });

  it('no headings while nobody owns a project', () => {
    const split = splitForPeople(
      [chat('a', 'p-site')],
      [{ id: 'p-site', name: 'сайт' }],
      artem,
      new Set(),
      labels,
    );
    expect(split.projects).toEqual([{ id: 'p-site', name: 'сайт' }]);
    expect(split.folds).toEqual([]);
  });
});
