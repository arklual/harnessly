// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import GitLabTab from '@/components/agents/inspector/gitlab-tab';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('GitLabTab', () => {
  it('says GitLab is not reachable instead of looking for ever when the look fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ error: 'Service unavailable' }), { status: 503 })),
    );
    render(<GitLabTab paths={['/repo']} jsonlPath="/t.jsonl" mentioned={[]} />);
    expect(await screen.findByText(/GitLab (сейчас недоступен|is not reachable)/)).toBeTruthy();
  });

  it('shows the empty state once a look answers', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ pushes: [], mrs: [] }), { status: 200 })),
    );
    render(<GitLabTab paths={['/repo']} jsonlPath="/t.jsonl" mentioned={[]} />);
    expect(await screen.findByText(/(Пушей|No pushes)/)).toBeTruthy();
  });
});
