import { afterEach, describe, expect, it } from 'vitest';
import { NEW_CHAT } from '@/next/stores/drafts';
import {
  MENTION_KIND,
  clearMentions,
  mentionsStore,
  removeMention,
  setMentions,
  withMentions,
  type Mention,
} from '@/next/stores/mentions';

const task: Mention = {
  id: 'ABC-2628',
  kind: MENTION_KIND.task,
  label: 'ABC-2628 · An epic',
  url: 'https://tracker.example.com/issue/ABC-2628',
};
const mr: Mention = {
  id: 'https://gitlab.example.com/team/api/-/merge_requests/502',
  kind: MENTION_KIND.mr,
  label: '!502 · A merge request',
  url: 'https://gitlab.example.com/team/api/-/merge_requests/502',
};

describe('mentions', () => {
  afterEach(() => clearMentions(NEW_CHAT));

  it('are the field’s chips as they were set, and an empty set leaves none', () => {
    setMentions(NEW_CHAT, [task, mr]);
    expect(mentionsStore.get(NEW_CHAT)).toEqual([task, mr]);
    setMentions(NEW_CHAT, [mr]);
    expect(mentionsStore.get(NEW_CHAT)).toEqual([mr]);
    setMentions(NEW_CHAT, []);
    expect(mentionsStore.has(NEW_CHAT)).toBe(false);
  });

  it('lose a chip by its id, and the field’s entry with the last one', () => {
    setMentions(NEW_CHAT, [task, mr]);
    removeMention(NEW_CHAT, task.id);
    expect(mentionsStore.get(NEW_CHAT)).toEqual([mr]);
    removeMention(NEW_CHAT, mr.id);
    expect(mentionsStore.has(NEW_CHAT)).toBe(false);
  });

  it('lead the message as lines with their links', () => {
    expect(withMentions([task, mr], 'Take a look')).toBe(
      `${task.label} — ${task.url}\n${mr.label} — ${mr.url}\n\nTake a look`,
    );
    expect(withMentions([task], '')).toBe(`${task.label} — ${task.url}`);
    expect(withMentions([], 'Just text')).toBe('Just text');
  });
});
