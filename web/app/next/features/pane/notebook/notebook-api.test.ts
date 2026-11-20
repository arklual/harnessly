import { afterEach, describe, expect, it, vi } from 'vitest';
import { notebookRequest } from './notebook-api';

vi.mock('@/next/core/net/machine', () => ({
  machineApi: (machine: string) => ({ http: (path: string) => `/machines/${machine}/api/v2${path}` }),
}));
afterEach(() => vi.unstubAllGlobals());
const runtime = { machine: 'machine-a' as never, conv: 'chat-a' as never };

describe('notebook API transport', () => {
  it('routes through the selected machine and conversation without starting on status reads', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ state: 'stopped' })));
    vi.stubGlobal('fetch', fetch);
    expect(await notebookRequest(runtime, 'a notebook.ipynb')).toEqual({ state: 'stopped' });
    expect(fetch).toHaveBeenCalledWith(
      '/machines/machine-a/api/v2/conversations/chat-a/notebook?path=a%20notebook.ipynb',
      { credentials: 'same-origin' },
    );
  });
  it('sends code as JSON, not a command or notebook kernelspec', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{}'));
    vi.stubGlobal('fetch', fetch);
    await notebookRequest(runtime, 'x.ipynb', 'execute', 'print("hello")');
    const options = fetch.mock.calls[0][1];
    expect(fetch.mock.calls[0][0]).toContain('&timeout=150');
    expect(options.method).toBe('POST');
    expect(JSON.parse(options.body)).toEqual({ action: 'execute', code: 'print("hello")' });
  });
  it('shows actionable backend failures instead of hiding them behind an HTTP code', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ error: 'Install jupyter_client and ipykernel' }), { status: 503 }),
        ),
    );
    await expect(notebookRequest(runtime, 'x.ipynb', 'execute', '1')).rejects.toThrow(
      'Install jupyter_client and ipykernel',
    );
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ message: 'No access to this notebook' }), { status: 403 }),
        ),
    );
    await expect(notebookRequest(runtime, 'x.ipynb')).rejects.toThrow('No access to this notebook');
  });
});
