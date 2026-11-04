import { describe, expect, it } from 'vitest';
import { DELIVERED_VIEW, deliveredViewOf } from '@/next/features/pane/delivered-kind';

describe('how the pane shows a file that is not text', () => {
  it('tells an archive, a picture, a PDF and a file that is only saved; text goes to the editor', () => {
    expect(deliveredViewOf('/tmp/Report.ZIP')).toBe(DELIVERED_VIEW.zip);
    expect(deliveredViewOf('a/b/photo.jpeg')).toBe(DELIVERED_VIEW.image);
    expect(deliveredViewOf('/w/contract.pdf')).toBe(DELIVERED_VIEW.pdf);
    expect(deliveredViewOf('/w/contract.docx')).toBe(DELIVERED_VIEW.docx);
    expect(deliveredViewOf('/w/old.doc')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/deck.pptx')).toBe(DELIVERED_VIEW.pptx);
    expect(deliveredViewOf('/w/old.ppt')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/cartoon.MP4')).toBe(DELIVERED_VIEW.video);
    expect(deliveredViewOf('/w/voice.flac')).toBe(DELIVERED_VIEW.audio);
    expect(deliveredViewOf('/w/old.avi')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/report.XLSX')).toBe(DELIVERED_VIEW.sheet);
    expect(deliveredViewOf('/w/list.csv')).toBe(DELIVERED_VIEW.sheet);
    expect(deliveredViewOf('/w/old.xls')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/notes.md')).toBeNull();
    expect(deliveredViewOf('/w/index.html')).toBeNull();
    expect(deliveredViewOf('/w/Makefile')).toBeNull();
    expect(deliveredViewOf('/w/.zip')).toBeNull();
  });

  it('lists a tar plain or gzipped, shows a font, and reads an svg as text (HRN-679)', () => {
    expect(deliveredViewOf('/w/site.tar.gz')).toBe(DELIVERED_VIEW.zip);
    expect(deliveredViewOf('/w/site.tgz')).toBe(DELIVERED_VIEW.zip);
    expect(deliveredViewOf('/w/site.tar')).toBe(DELIVERED_VIEW.zip);
    expect(deliveredViewOf('/w/log.gz')).toBe(DELIVERED_VIEW.save);
    expect(deliveredViewOf('/w/Inter.woff2')).toBe(DELIVERED_VIEW.font);
    expect(deliveredViewOf('/w/Brand.OTF')).toBe(DELIVERED_VIEW.font);
    expect(deliveredViewOf('/w/logo.svg')).toBeNull();
  });
});
