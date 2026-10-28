import { describe, expect, it } from 'vitest';
import {
  readDeckExportJob,
  readDeckPptxStyle,
  readDeckRecording,
  readDeckPdfAnswer,
  readDeckState,
  readDeckTidied,
} from '@/next/features/designly/deck/wire';
import { deckNotesSeconds } from '@/next/features/designly/deck/notes-len';

const slide = {
  id: '01JSLIDE',
  path: 'project/Cover.dc.html',
  title: 'Cover',
  w: 1920,
  h: 1080,
  notes: 'Hello',
  voice: true,
  seconds: 10,
  version: 'lq3k-4f',
  blank: true,
};
const comment = {
  id: '01JCOMMENT',
  page: '01JSLIDE',
  x: 74,
  y: 30,
  label: 'Схема',
  text: 'Bigger, please',
  by: { name: 'Alex', email: 'alex@example.org' },
  at: '2026-10-27T10:00:00Z',
  resolved: false,
  resolvedBy: null,
  resolvedAt: null,
};

describe('readDeckState', () => {
  it('reads a whole deck as the runtime answers it', () => {
    const state = readDeckState({
      rev: 1761559200000,
      settings: {
        v: 1,
        minutes: 15,
        slides: 8,
        tone: 'biz',
        script: true,
        style: { source: 'acme.pptx', system: null, layouts: 9 },
        theme: 'co',
        font: 'golos',
      },
      chrome: true,
      ffmpeg: false,
      slides: [slide],
      comments: [comment],
      guestResolved: ['g-12'],
      recordings: [
        { id: '01JREC', at: '2026-10-27T10:00:00Z', durationMs: 372000, state: 'done', retaken: [3] },
      ],
      published: {
        video: true,
        pdf: true,
        pdfAuto: true,
        pdfBuilding: false,
        pdfAt: '2026-10-27T10:00:00Z',
        pdfError: null,
      },
    });
    expect(state).toEqual({
      rev: 1761559200000,
      settings: {
        v: 1,
        minutes: 15,
        slides: 8,
        tone: 'biz',
        script: true,
        style: { source: 'acme.pptx', system: null, layouts: 9 },
        theme: 'co',
        font: 'golos',
      },
      chrome: true,
      ffmpeg: false,
      slides: [slide],
      comments: [comment],
      guestResolved: ['g-12'],
      recordings: [
        { id: '01JREC', at: '2026-10-27T10:00:00Z', durationMs: 372000, state: 'done', retaken: [3] },
      ],
      published: {
        video: true,
        pdf: true,
        pdfAuto: true,
        pdfBuilding: false,
        pdfAt: '2026-10-27T10:00:00Z',
        pdfError: null,
      },
    });
  });

  it('reads Go nulls as empty lists and unknown values as the defaults', () => {
    const state = readDeckState({
      rev: 5,
      settings: { tone: 'loud', font: 'comic', slides: 'many' },
      slides: null,
      comments: null,
      guestResolved: null,
      recordings: null,
    });
    expect(state?.slides).toEqual([]);
    expect(state?.comments).toEqual([]);
    expect(state?.recordings).toEqual([]);
    expect(state?.settings).toMatchObject({
      tone: 'talk',
      font: 'inter',
      slides: null,
      script: true,
      minutes: 10,
    });
    expect(state?.published).toEqual({
      video: false,
      pdf: false,
      pdfAuto: false,
      pdfBuilding: false,
      pdfAt: null,
      pdfError: null,
    });
  });

  it('leaves out malformed items and clamps a pin into the slide', () => {
    const state = readDeckState({
      slides: [slide, { title: 'no id' }, 7],
      comments: [{ ...comment, x: 140, y: -3 }, null],
    });
    expect(state?.slides.map((s) => s.id)).toEqual(['01JSLIDE']);
    expect(state?.comments[0]).toMatchObject({ x: 100, y: 0 });
  });

  it('keeps only --deck-* variables of a theme', () => {
    const state = readDeckState({
      settings: {
        themes: [
          { id: 'x', name: 'X', vars: { '--deck-bg': '#fff', color: 'red', '--deck-acc': 3 } },
          { name: 'no id' },
        ],
      },
    });
    expect(state?.settings.themes).toEqual([{ id: 'x', name: 'X', vars: { '--deck-bg': '#fff' } }]);
  });

  it('is null for an answer that is not a deck', () => {
    expect(readDeckState(null)).toBeNull();
    expect(readDeckState([1])).toBeNull();
    expect(readDeckState('deck')).toBeNull();
  });
});

describe('readDeckPdfAnswer', () => {
  it('reads the published state the PDF route answers', () => {
    expect(readDeckPdfAnswer({ published: { pdf: false, pdfAuto: true, pdfBuilding: true } })).toEqual({
      video: false,
      pdf: false,
      pdfAuto: true,
      pdfBuilding: true,
      pdfAt: null,
      pdfError: null,
    });
  });

  it('is null for an answer without the published state', () => {
    expect(readDeckPdfAnswer({ ok: true })).toBeNull();
    expect(readDeckPdfAnswer(null)).toBeNull();
  });
});

describe('readDeckRecording', () => {
  it('reads the summary, the segments and the montage', () => {
    const rec = readDeckRecording({
      summary: {
        id: '01JREC',
        startedAt: '2026-10-27T10:00:00Z',
        durationMs: 60000,
        camera: true,
        mime: 'video/webm',
        slides: ['a', 'b'],
        takes: [{ page: 'a', n: 1, startMs: 0, endMs: 30000 }],
        state: 'done',
      },
      segments: [
        {
          t0: 0,
          t1: 4000,
          page: 'a',
          parts: [
            { text: 'Привет!', cut: false, kind: 'word', t0: 0, t1: 900 },
            { text: 'э-э', cut: true, kind: 'filler', t0: 900, t1: 1300 },
          ],
        },
      ],
      edit: { cut: true, subs: false, kept: ['0.1'], cursor: 'smooth', cam: 'tl' },
      cutMs: 400,
      retaken: [2],
    });
    expect(rec?.segments[0].parts[1]).toEqual({ text: 'э-э', cut: true, kind: 'filler', t0: 900, t1: 1300 });
    expect(rec?.edit).toEqual({ cut: true, subs: false, kept: ['0.1'], cursor: 'smooth', cam: 'tl' });
    expect(rec?.summary.takes).toHaveLength(1);
    expect(rec?.retaken).toEqual([2]);
  });

  it('is null without a summary', () => {
    expect(readDeckRecording({ segments: [] })).toBeNull();
  });
});

describe('readDeckExportJob', () => {
  it('reads a job and clamps its progress', () => {
    expect(
      readDeckExportJob({
        id: 'j1',
        format: 'pdf',
        state: 'running',
        progress: 1.4,
        file: null,
        error: null,
        at: 'now',
      }),
    ).toEqual({
      id: 'j1',
      format: 'pdf',
      state: 'running',
      progress: 1,
      file: null,
      error: null,
      at: 'now',
    });
  });

  it('refuses an unknown format', () => {
    expect(readDeckExportJob({ id: 'j1', format: 'docx' })).toBeNull();
  });
});

describe('the other answers', () => {
  it('reads a template style', () => {
    expect(
      readDeckPptxStyle({
        name: 'Acme',
        colors: ['#fff', 3],
        fonts: { head: 'Inter' },
        layouts: 9,
        vars: { '--deck-acc': '#ffdd2d', x: 'y' },
      }),
    ).toEqual({
      name: 'Acme',
      colors: ['#fff'],
      fonts: { head: 'Inter', body: '' },
      layouts: 9,
      vars: { '--deck-acc': '#ffdd2d' },
    });
  });

  it('reads tidied notes, leaving out a slide without an id', () => {
    expect(readDeckTidied([{ id: 'a', notes: 'Text', seconds: 15, voice: true }, { notes: 'lost' }])).toEqual(
      [{ id: 'a', notes: 'Text', seconds: 15, voice: true }],
    );
  });

  it('counts the notes the way the runtime does', () => {
    expect(deckNotesSeconds('')).toBe(10);
    expect(deckNotesSeconds('word '.repeat(66))).toBe(30);
  });
});
