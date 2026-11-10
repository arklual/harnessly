// HRN-905: a bash-mode line the person typed («! git push …») reads as a command run, not as «command /! git push».
import { describe, expect, it } from 'vitest';
import { stepView, toolView } from '@/next/features/chat/steps';
import type { ToolEntry } from '@/next/features/chat/types';

const command = (summary: string) =>
  ({ t: 'tool', id: 'c:1#1', tool: 'command', summary }) as unknown as ToolEntry;

describe('a command the person typed', () => {
  it('a bash-mode line runs its command', () => {
    const view = stepView(command('/! git push github 9cbbf804:refs/tags/v0.16.196'));
    expect(view.detail).toBe('git push github 9cbbf804:refs/tags/v0.16.196');
    expect(view.icon).toBe(toolView('Bash').icon);
    expect(view.label).toBe(toolView('Bash').label);
  });

  it('a slash command reads as it was typed', () => {
    expect(stepView(command('/compact')).detail).toBe('/compact');
    expect(stepView(command('/!x')).detail).toBe('/!x');
  });
});
