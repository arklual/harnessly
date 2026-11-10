// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest';
import { TRACE_HEADER } from '@/next/core/journal';
import {
  frameReason,
  TRACE_PARAM,
  withTraceParam,
  type IFrameSeen,
} from '@/next/features/app-views/view-journal';

const seen = (src: string, loaded = true): IFrameSeen => ({
  src,
  at: 1,
  loadedAt: loaded ? 2 : null,
  csp: null,
});

const answer = (r: Partial<Response>) => vi.fn(() => Promise.resolve(r as Response));

describe('the page tells why a frame the machine never saw did not connect (HRN-976)', () => {
  it('asks a frame of its own origin again: the status and the gateway code, with the trace', async () => {
    const fetcher = answer({
      type: 'basic',
      ok: false,
      status: 502,
      json: () => Promise.resolve({ code: 'machine_offline' }),
    });
    const r = await frameReason(seen('/m/abc/__pv/1-x/ui/index.html'), 'v-1', fetcher);
    expect(r.status).toBe(502);
    expect(r.text).toContain('502');
    expect(r.text).toContain('machine_offline');
    const init = fetcher.mock.calls[0] as unknown as [string, RequestInit];
    expect(new Headers(init[1].headers).get(TRACE_HEADER)).toBe('v-1');
    expect(init[1].redirect).toBe('manual');
  });

  it('names a redirect (a sign-in) and a policy refusal', async () => {
    const redirect = await frameReason(
      seen('/m/abc/__pv/1-x/'),
      'v-1',
      answer({ type: 'opaqueredirect', ok: false, status: 0 }),
    );
    expect(redirect.text).not.toBe('');
    const csp = await frameReason({ ...seen('https://slot.example/x'), csp: 'frame-src' }, 'v-1');
    expect(csp.text).toContain('frame-src');
  });

  it('cannot read another origin: says whether the frame loaded at all', async () => {
    const fetcher = vi.fn();
    const loaded = await frameReason(seen('https://slot.example/x'), 'v-1', fetcher);
    const not = await frameReason(seen('https://slot.example/x', false), 'v-1', fetcher);
    expect(fetcher).not.toHaveBeenCalled();
    expect(loaded.text).toContain('slot.example');
    expect(not.text).toContain('slot.example');
    expect(loaded.text).not.toBe(not.text);
  });

  it('passes a frame its own origin answered well: nothing to add', async () => {
    const r = await frameReason(
      seen('/m/abc/__pv/1-x/'),
      'v-1',
      answer({ type: 'basic', ok: true, status: 200 }),
    );
    expect(r.text).toBe('');
  });

  it('puts the trace in the address', () => {
    expect(withTraceParam('https://slot.example/a?x=1', 'v-9')).toBe(
      `https://slot.example/a?x=1&${TRACE_PARAM}=v-9`,
    );
    expect(withTraceParam('/m/a/__pv/1-x/p', 'v-9')).toBe(`/m/a/__pv/1-x/p?${TRACE_PARAM}=v-9`);
  });
});
