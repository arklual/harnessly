import { describe, expect, it } from 'vitest';
import { docPlaceOf, docSlugOf } from '@/next/features/pane/doc-link';
import type { IInstalledApp } from '@/next/features/app-views/api';

const ORIGIN = 'https://agents.example.com';

const docs = (over: Partial<IInstalledApp> = {}): IInstalledApp =>
  ({
    id: 'harnessly.docs',
    name: 'Документы',
    description: '',
    version: '1.0.0',
    trust: 'sandboxed',
    targets: ['app', 'pane'],
    permissions: [],
    views: [{ id: 'docs', title: 'Документы', targets: ['app', 'pane'] }],
    mcpServers: [],
    state: 'ready',
    ...over,
  }) as IInstalledApp;

describe('docPlaceOf: a document opens in the documents app in the pane (HRN-1484)', () => {
  it('reads the slug of the reader path and of a full address of this page', () => {
    expect(docSlugOf('/agents/docs/research-uuc5gf', ORIGIN)).toBe('research-uuc5gf');
    expect(docSlugOf(`${ORIGIN}/agents/docs/research-uuc5gf/`, ORIGIN)).toBe('research-uuc5gf');
    expect(docSlugOf('/docs/plan', ORIGIN)).toBe('plan');
  });
  it('leaves the reader what only it does, and other services', () => {
    expect(docSlugOf('/agents/docs/plan?v=3', ORIGIN)).toBeNull();
    expect(docSlugOf('/agents/docs/plan?thread=t1', ORIGIN)).toBeNull();
    expect(docSlugOf('/agents/docs/x-abc', ORIGIN)).toBeNull();
    expect(docSlugOf('https://other.example.com/agents/docs/plan', ORIGIN)).toBeNull();
    expect(docSlugOf('/agents/docs/plan/edit', ORIGIN)).toBeNull();
  });
  it('names the app and its pane view, none when the app cannot show it', () => {
    expect(docPlaceOf('/agents/docs/plan', [docs()], ORIGIN)).toEqual({
      app: 'harnessly.docs',
      view: 'docs',
      slug: 'plan',
    });
    expect(docPlaceOf('/agents/docs/plan', [docs({ off: true })], ORIGIN)).toBeNull();
    expect(
      docPlaceOf(
        '/agents/docs/plan',
        [docs({ views: [{ id: 'docs', title: '', targets: ['app'] }] })],
        ORIGIN,
      ),
    ).toBeNull();
    expect(docPlaceOf('/agents/docs/plan', [], ORIGIN)).toBeNull();
  });
});
