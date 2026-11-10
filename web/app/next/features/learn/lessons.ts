// The lessons of «Обучение» (HRN-792): each one a short film recorded from the real interface (e2e/lesson-videos.spec.ts)
// and a hands-on tour of the same name (features/tour/tracks.ts). The films are the preview; the tour is the lesson.
// Texts live in next-learn.json: `<id>.title`, `<id>.desc` and `<id>.c<n>` for the n-th chapter.

const MEDIA = import.meta.glob<string>('./media/*.{mp4,jpg}', {
  eager: true,
  query: '?url',
  import: 'default',
});

export const LEARN_SECTIONS = ['base', 'syn', 'more'] as const;
export type TLearnSection = (typeof LEARN_SECTIONS)[number];

export interface ILesson {
  id: string;
  section: TLearnSection;
  /** The film's length, s. */
  dur: number;
  /** When each chapter of the film begins, s; its caption is `<id>.c<n>`. */
  chapters: readonly number[];
}

export const LESSONS: readonly ILesson[] = [
  { id: 'chat', section: 'base', dur: 40.08, chapters: [4.35, 10.01, 14.66, 19.68, 23.19, 29.75] },
  { id: 'work', section: 'base', dur: 34.28, chapters: [1.5, 9.32, 13.85, 23.42] },
  { id: 'pane', section: 'base', dur: 33, chapters: [6.7, 11.83, 20.89] },
  { id: 'site', section: 'syn', dur: 62.24, chapters: [4.57, 15.99, 23.23, 34.34, 39.79, 44.71] },
  { id: 'slides', section: 'syn', dur: 55.84, chapters: [4.48, 10.54, 30.05, 37.3, 41.42] },
  { id: 'video', section: 'syn', dur: 50.08, chapters: [3.47, 14.78, 21.08, 31.75] },
  { id: 'ds', section: 'syn', dur: 24.84, chapters: [3.2, 6.4, 12.9] },
  { id: 'safe', section: 'more', dur: 17.4, chapters: [2.39, 7.71] },
  { id: 'agents', section: 'more', dur: 31.12, chapters: [1.49, 10.33, 14.57] },
  { id: 'extra', section: 'more', dur: 36, chapters: [1.74, 12.79, 20.48, 27.05] },
];

export const lessonById = (id: string): ILesson | null => LESSONS.find((l) => l.id === id) ?? null;

/** The lesson after this one, round to the first. */
export const nextLesson = (id: string): ILesson => {
  const i = LESSONS.findIndex((l) => l.id === id);
  return LESSONS[(i + 1) % LESSONS.length] as ILesson;
};

const media = (id: string, ext: 'mp4' | 'jpg'): string => {
  const url = MEDIA[`./media/${id}.${ext}`];
  if (!url) throw new Error(`no ${ext} for the lesson ${id}`);
  return url;
};
export const filmOf = (id: string) => media(id, 'mp4');
export const posterOf = (id: string) => media(id, 'jpg');

/** The chapter playing at second t: the last one begun, the first before it. */
export const chapterAt = (lesson: ILesson, t: number): number => {
  let at = 0;
  lesson.chapters.forEach((c, i) => {
    if (c <= t + 0.05) at = i;
  });
  return at;
};

/** m:ss of a second. */
export const clock = (s: number): string => {
  const v = Math.max(0, Math.floor(s));
  return `${Math.floor(v / 60)}:${String(v % 60).padStart(2, '0')}`;
};
