import { describe, expect, it, vi } from 'vitest';

const config = vi.hoisted(() => ({ taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}' }));
vi.mock('@/platform/config', () => ({ appConfig: () => config }));

import { chatIdOfHref, docOfCode, splitRefs } from '@/next/features/chat/refs';

describe('splitRefs', () => {
  it('finds tasks and paths in place; a bare merge request number stays a word', () => {
    expect(splitRefs('Fixed ABC-3142 in !87, see web/app/next/feed.tsx:12.')).toEqual([
      { text: 'Fixed ' },
      { ref: 'task', label: 'ABC-3142' },
      { text: ' in !87, see ' },
      { ref: 'file', label: 'web/app/next/feed.tsx:12' },
      { text: '.' },
    ]);
  });

  it('leaves plain words with dots, exclamations and lone file names as text', () => {
    expect(splitRefs('Done! Version 1.2 of feed.tsx, e.g. this.')).toEqual([
      { text: 'Done! Version 1.2 of feed.tsx, e.g. this.' },
    ]);
  });

  it("finds tasks when the tracker's key pattern has groups of its own", () => {
    config.taskKeyPattern = '(ABC|EXP|OPS)-[0-9]{1,6}';
    expect(splitRefs('admin-api !9 and OPS-244')).toEqual([
      { text: 'admin-api !9 and ' },
      { ref: 'task', label: 'OPS-244' },
    ]);
  });
});

describe('docOfCode', () => {
  const docs = new Set(['/home/dev/app/docs/plan.md', '/home/dev/app/notes.txt']);

  it('opens the touched Markdown file a path in code names, however much of its path is written', () => {
    expect(docOfCode('docs/plan.md', docs)).toBe('/home/dev/app/docs/plan.md');
    expect(docOfCode('/home/dev/app/docs/plan.md:12', docs)).toBe('/home/dev/app/docs/plan.md');
    expect(docOfCode('plan.md', docs)).toBe('/home/dev/app/docs/plan.md');
  });

  it('leaves code that is not such a path alone', () => {
    expect(docOfCode('docs/other.md', docs)).toBeNull();
    expect(docOfCode('notes.txt', docs)).toBeNull();
    expect(docOfCode('docs/plan.md and more', docs)).toBeNull();
    expect(docOfCode('docs/plan.md', undefined)).toBeNull();
  });
});

// a chat the agent names — by its id, its machine's ref or its address — is a chat, not «nothing to open».
describe('chats in the text', () => {
  it('finds a bare chat id, not one inside a path or a word', () => {
    expect(splitRefs('Запустил tab-vh0l85vgd, см. /c/tab-vh0l85vgd и my-tab-vh0l85vgd')).toEqual([
      { text: 'Запустил ' },
      { ref: 'chat', label: 'tab-vh0l85vgd' },
      { text: ', см. /c/tab-vh0l85vgd и my-tab-vh0l85vgd' },
    ]);
  });

  it('reads a chat from a link: its id, its machine ref, its address on this site', () => {
    const here = 'https://ai.example.com';
    expect(chatIdOfHref('tab-vh0l85vgd')).toBe('tab-vh0l85vgd');
    expect(chatIdOfHref('dev-department.example.com/tab-pwn0vy44r')).toBe('tab-pwn0vy44r');
    expect(chatIdOfHref('dev-department-example-com/tab-pwn0vy44r')).toBe('tab-pwn0vy44r');
    expect(chatIdOfHref('https://ai.example.com/c/tab-vh0l85vgd', here)).toBe('tab-vh0l85vgd');
    expect(chatIdOfHref('/c/tab-vh0l85vgd', here)).toBe('tab-vh0l85vgd');
    expect(chatIdOfHref('https://ai.example.com/?c=agent:m1:tab-vh0l85vgd', here)).toBe('tab-vh0l85vgd');
  });

  it('leaves other links alone, and another site’s /c/tab-… is that site’s', () => {
    const here = 'https://ai.example.com';
    expect(chatIdOfHref('https://other.example.com/c/tab-vh0l85vgd', here)).toBeNull();
    expect(chatIdOfHref('https://ai.example.com/designly', here)).toBeNull();
    expect(chatIdOfHref('https://gitlab.example.com/a/b/-/merge_requests/80', here)).toBeNull();
    expect(chatIdOfHref('tab-x')).toBeNull();
  });
});
