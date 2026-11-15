import { describe, expect, it } from 'vitest';
import { startFolder } from '@/next/bindings/home-composer';
import type { Conversation } from '@/next/protocol/v2';

const conv = (id: string, cwd: string, createdAt: number, parentId?: string) =>
  ({ id, machineId: 'm1', cwd, createdAt, ...(parentId ? { parentId } : {}) }) as unknown as Conversation;

describe('startFolder', () => {
  it('does not follow a delegated chat into its folder, nor a chat that once did', () => {
    const convs = [
      conv('a', '/work/app', 1),
      conv('sub', '/work/app/podchaty/docs', 3, 'a'),
      conv('b', '/work/app/podchaty/docs', 4),
    ];
    expect(startFolder(convs, 'm1' as never, '/home')).toBe('/work/app');
  });
  it('falls back to the machine default', () => {
    expect(startFolder([conv('sub', '/x/podchaty/y', 1, 'p')], 'm1' as never, '/home')).toBe('/home');
  });
});
