import { describe, expect, it } from 'vitest';
import { docsAppPath, docsReaderPath } from './docs-redirect';

describe('the reader address of a document', () => {
  it('opens the documents app with the document', () => {
    expect(docsAppPath('/docs/plan-x1', '')).toBe('/apps/harnessly.docs?o=plan-x1');
    expect(docsAppPath('/docs/plan-x1/', '?from=push')).toBe('/apps/harnessly.docs?o=plan-x1');
  });

  it('keeps the reader for a version, a thread and the fallback', () => {
    expect(docsAppPath('/docs/plan-x1', '?v=abc')).toBeNull();
    expect(docsAppPath('/docs/plan-x1', '?thread=t1')).toBeNull();
    expect(docsAppPath('/docs/plan-x1', '?focus=t1')).toBeNull();
    expect(docsAppPath('/docs/plan-x1', '?legacy=1')).toBeNull();
    expect(docsReaderPath('plan-x1')).toBe('/docs/plan-x1?legacy=1');
  });

  it('leaves other addresses and odd slugs alone', () => {
    expect(docsAppPath('/apps', '')).toBeNull();
    expect(docsAppPath('/docs/a%2Fb', '')).toBeNull();
    expect(docsAppPath('/docs/%E0%A4%A', '')).toBeNull();
  });
});
