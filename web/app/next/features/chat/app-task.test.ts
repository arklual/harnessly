import { describe, expect, it } from 'vitest';
import { parseAppTask } from '@/next/features/chat/app-task';

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
