// Chapters of the published video as the guest page uses them. Pure.
import type { IGuestBoard } from '@/next/features/designly/guest/guest-config';

/** A chapter as the runtime publishes it (`chapters.json`, T in ms): the slide's page id, its title and where it starts. */
export interface IPublishedChapter {
  page: string;
  title: string;
  t: number;
}

/** A chapter tied to the slide it is about. */
export interface IChapterOfSlide {
  /** Index of the slide in the deck. */
  slide: number;
  title: string;
  t: number;
}

/** The chapters whose slide is in the link, in the order of the video; a chapter of a slide the guest cannot see is left out. */
export const chaptersOf = (
  chapters: readonly IPublishedChapter[],
  boards: readonly Pick<IGuestBoard, 'id' | 'title'>[],
): IChapterOfSlide[] =>
  chapters
    .flatMap((c) => {
      const slide = boards.findIndex((b) => b.id === c.page);
      return slide < 0 ? [] : [{ slide, title: c.title || boards[slide].title, t: c.t }];
    })
    .sort((a, b) => a.t - b.t);

/** The chapter a time (ms) is in: the last one that has started; null before the first or without chapters. */
export const currentChapter = (starts: readonly number[], ms: number): number | null => {
  let found: number | null = null;
  starts.forEach((start, i) => {
    if (start <= ms && (found === null || start >= starts[found])) found = i;
  });
  return found;
};

/** m:ss (h:mm:ss over an hour) of a time in ms. */
export const clock = (ms: number): string => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
};
