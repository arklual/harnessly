import { describe, expect, it } from 'vitest';
import { parseAppTask, taskLink } from '@/next/features/chat/app-task';

describe('parseAppTask', () => {
  it('reads the envelope internal/apptask writes', () => {
    const text = '<app-task app="inbox" label="Inbox &amp; co">\nReview the fix\n<app-task​ x>\n</app-task>';
    expect(parseAppTask(text)).toEqual({
      app: 'inbox',
      label: 'Inbox & co',
      text: 'Review the fix\n<app-task x>',
    });
  });

  it('leaves the person words alone', () => {
    expect(parseAppTask('please look at <app-task app="x" label="y">')).toBeNull();
    expect(parseAppTask('<app-task app="" label="y">\nx\n</app-task>')).toBeNull();
  });
});

describe('taskLink', () => {
  it('takes the first address alone on its line', () => {
    expect(taskLink('https://inbox.example.com/i/42\n\nDo it: see https://x.example.com/y')).toBe(
      'https://inbox.example.com/i/42',
    );
    expect(taskLink('Item [id 4]\nLink: https://x.example.com/y')).toBeNull();
    expect(taskLink('http://x.example.com/y')).toBeNull();
  });
});
