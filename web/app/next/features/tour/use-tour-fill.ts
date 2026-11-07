// A step of the tour that asks the agent puts its words into the field the person is looking at, once and only into
// an empty field: the person reads it and sends it. The design's chat takes the step's prompt, a block's note its note.
import { useEffect } from 'react';
import { useTourStep } from '@/next/features/tour/use-tour-step';

export const useTourFill = (
  text: string,
  setText: (value: string) => void,
  field: 'prompt' | 'note' = 'prompt',
) => {
  const words = useTourStep()?.[field] || null;
  useEffect(() => {
    if (words && text === '') setText(words);
    // Only when the step comes: a field the person emptied stays empty.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [words]);
};
