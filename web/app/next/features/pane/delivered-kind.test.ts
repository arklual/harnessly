import { describe, expect, it } from 'vitest';
import { DELIVERED_VIEW, deliveredViewOf } from '@/next/features/pane/delivered-kind';

describe('how the pane shows a file that is not text', () => {
  it('tells an archive, a picture, a PDF and a file that is only saved; text goes to the editor', () => {
    expect(deliveredViewOf('/tmp/Report.ZIP')).toBe(DELIVERED_VIEW.zip);
    expect(deliveredViewOf('a/b/photo.jpeg')).toBe(DELIVERED_VIEW.image);
    expect(deliveredViewOf('/w/contract.pdf')).toBe(DELIVERED_VIEW.pdf);
    expect(deliveredViewOf('/w/contract.docx')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/notes.md')).toBeNull();
    expect(deliveredViewOf('/w/index.html')).toBeNull();
    expect(deliveredViewOf('/w/Makefile')).toBeNull();
    expect(deliveredViewOf('/w/.zip')).toBeNull();
  });
});
