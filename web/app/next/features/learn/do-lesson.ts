// A lesson by hand starts from the greeting: every tour's first step is reachable from there.
import { HOME, goTo } from '@/next/routes/routes';
import { startTour } from '@/next/stores/tour';

export const doLesson = (id: string) => {
  goTo(HOME);
  startTour(id);
};
