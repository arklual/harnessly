import { createContext, useContext } from 'react';

interface ITimelineScroll {
  holdPosition: () => void;
}

export const TimelineScrollContext = createContext<ITimelineScroll>({ holdPosition: () => {} });

/** Call before a user-initiated expand so the pinned-to-bottom spring does not drag the view. */
export const useHoldTimelinePosition = () => useContext(TimelineScrollContext).holdPosition;
