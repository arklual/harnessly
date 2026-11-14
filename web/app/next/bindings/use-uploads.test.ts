// @vitest-environment jsdom
// a picture pasted twice (or by two fields of one draft) is attached once; a message sends each file once.
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const toast = vi.fn();
vi.mock('@/next/ui/toast', () => ({ toast: (...args: unknown[]) => toast(...args) }));

import { distinctRefs, useUploads } from '@/next/bindings/use-uploads';
import { t } from '@/next/i18n';
import type { AttachmentRef } from '@/next/protocol/v2';
import type { DraftKey } from '@/next/stores/drafts';

const picture = (bytes: number[], name: string) => new File([new Uint8Array(bytes)], name, { type: 'image/png' });
let n = 0;
// No machine: the rows stand with their reason and nothing uploads, which is all these tests need.
const draft = (): DraftKey => `design:paste-${(n += 1)}`;

afterEach(() => toast.mockReset());

describe('the files of one message', () => {
  it('take the same picture once, however often it comes, and say so', async () => {
    const { result } = renderHook(() => useUploads(null, draft(), []));
    act(() => {
      result.current.onFiles([picture([1, 2, 3], 'shot.png')]);
      result.current.onFiles([picture([1, 2, 3], 'shot.png')]);
    });
    await waitFor(() => expect(toast).toHaveBeenCalledWith(t('next.upload.already')));
    expect(result.current.attachments.map((a) => a.name)).toEqual(['shot.png']);
  });

  it('take two different pictures', async () => {
    const { result } = renderHook(() => useUploads(null, draft(), []));
    act(() => result.current.onFiles([picture([1], 'a.png'), picture([2], 'b.png')]));
    await waitFor(() => expect(result.current.attachments).toHaveLength(2));
    expect(toast).not.toHaveBeenCalled();
  });

  it('are one for two fields of the same draft (Sinopia: the folded field and the open chat)', async () => {
    const key = draft();
    const island = renderHook(() => useUploads(null, key, []));
    const chat = renderHook(() => useUploads(null, key, []));
    act(() => island.result.current.onFiles([picture([4, 4], 'a.png')]));
    await waitFor(() => expect(island.result.current.attachments).toHaveLength(1));
    act(() => chat.result.current.onFiles([picture([4, 4], 'a.png')]));
    await waitFor(() => expect(toast).toHaveBeenCalledTimes(1));
    expect(chat.result.current.attachments).toHaveLength(0);
  });

  it('take a picture again once it was removed', async () => {
    const { result } = renderHook(() => useUploads(null, draft(), []));
    act(() => result.current.onFiles([picture([5], 'a.png')]));
    await waitFor(() => expect(result.current.attachments).toHaveLength(1));
    act(() => result.current.onRemove(result.current.attachments[0].id));
    act(() => result.current.onFiles([picture([5], 'a.png')]));
    await waitFor(() => expect(result.current.attachments).toHaveLength(1));
    expect(toast).not.toHaveBeenCalled();
  });
});

describe('what a message sends', () => {
  const ref = (id: string): AttachmentRef => ({ id, name: `${id}.png`, kind: 'image', mime: 'image/png' });

  it('is each file once: by id and by what it holds', () => {
    const contents = new Map([
      ['a', 'sha-1'],
      ['b', 'sha-1'],
      ['c', 'sha-2'],
    ]);
    expect(distinctRefs([ref('a'), ref('a'), ref('b'), ref('c'), ref('d')], contents).map((r) => r.id)).toEqual([
      'a',
      'c',
      'd',
    ]);
  });
});
