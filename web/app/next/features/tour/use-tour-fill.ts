// A step of the tour that asks the agent puts its words into the field the person is looking at, once and only into
// an empty field: the person reads it and sends it. Each field that can take a request calls this with its own draft.
import { useEffect } from 'react';
import { trackOf } from '@/next/features/tour/tracks';
import { useTour } from '@/next/stores/tour';

export const useTourFill = (text: string, setText: (value: string) => void) => {
  const tour = useTour();
  const prompt = trackOf(tour.track)?.steps[tour.step]?.prompt || null;
  useEffect(() => {
    if (prompt && text === '') setText(prompt);
    // Only when the step comes: a field the person emptied stays empty.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prompt]);
};
