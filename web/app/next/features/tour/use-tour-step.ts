// The step of the tour on screen, for the features that serve it: the design's chat docks and takes the cheapest model,
// a field takes the step's words, the page rings the element the step is about. Null when no tour runs.
import { trackOf } from '@/next/features/tour/tracks';
import type { ITourStep } from '@/next/features/tour/model';
import { useTour } from '@/next/stores/tour';

export const useTourStep = (): ITourStep | null => {
  const tour = useTour();
  return trackOf(tour.track)?.steps[tour.step] ?? null;
};
