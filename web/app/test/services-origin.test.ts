import { describe, expect, it, vi } from 'vitest';

// The workspace of the compose file: the page on http://localhost:8022, boards and artifacts on other ports of the
// same host. The services are told apart by origin, not by host name.
vi.mock('@/platform/config', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/platform/config')>();
  return {
    ...real,
    appConfig: () => ({
      ...real.appConfig(),
      boardsUrl: 'http://localhost:8081',
      artifactsUrl: 'http://localhost:8082',
    }),
  };
});

const { boardFrameUrlOf, docSlugOf, paneDocTargetOf } = await import('@/lib/hl-doc-path');
const { presentLink } = await import('@/lib/presented-link');

describe('boards and documents on other ports of one host', () => {
  it('reads a document and a board by their own origin', () => {
    expect(docSlugOf('http://localhost:8082/a/plan-1a2b3c')).toBe('plan-1a2b3c');
    expect(docSlugOf('http://localhost:8081/b/flow-4d5e6f')).toBe('flow-4d5e6f');
    expect(docSlugOf('http://localhost:8083/a/plan-1a2b3c')).toBeNull();
    expect(docSlugOf('http://localhost:8082/b/flow-4d5e6f')).toBeNull();
  });

  it('frames a board with its key at the configured origin only', () => {
    expect(boardFrameUrlOf('http://localhost:8081/b/flow-4d5e6f?k=abc123')).toBe(
      'http://localhost:8081/b/flow-4d5e6f?k=abc123',
    );
    expect(boardFrameUrlOf('http://localhost:8081/b/flow-4d5e6f')).toBeNull();
    expect(boardFrameUrlOf('https://localhost:8081/b/flow-4d5e6f?k=abc123')).toBeNull();
    expect(boardFrameUrlOf('http://localhost:9999/b/flow-4d5e6f?k=abc123')).toBeNull();
  });

  it('opens them in the pane and presents them as a board and a document', () => {
    expect(paneDocTargetOf('http://localhost:8081/b/flow-4d5e6f?k=abc123')).toBe(
      'http://localhost:8081/b/flow-4d5e6f?k=abc123',
    );
    expect(paneDocTargetOf('http://localhost:8082/a/plan-1a2b3c')).toBe('/docs/plan-1a2b3c');
    expect(presentLink('http://localhost:8081/b/flow-4d5e6f?k=abc123')).toMatchObject({
      kind: 'artifact',
      board: true,
    });
    expect(presentLink('http://localhost:8082/a/plan-1a2b3c')).toMatchObject({ kind: 'artifact' });
    expect(presentLink('http://localhost:9999/a/plan-1a2b3c').kind).toBe('web');
  });
});
