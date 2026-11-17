// The tracker's mark on a task's card in the chat: its logo in white on its colour (chat.css, .hx-made__brand). Loaded
// with the first task card only, so the feed's chunk does not carry every brand of the Connections page.
import { BRAND_MARK, type IBrandMark } from '@/next/features/connections/brand-marks';

export type TTracker = 'youtrack' | 'jira' | 'linear' | 'github' | 'gitlab';

const TrackerMark = ({ tracker }: { tracker: TTracker }) => {
  const mark: IBrandMark = BRAND_MARK[tracker];
  return (
    <span className="hx-made__tile hx-made__brand" data-brand={tracker} aria-hidden="true">
      <svg
        viewBox={mark.box ?? '0 0 24 24'}
        fill="currentColor"
        fillRule={mark.evenOdd === true ? 'evenodd' : undefined}
        focusable="false"
      >
        {mark.d.map((d) => (
          <path key={d} d={d} />
        ))}
      </svg>
    </span>
  );
};

export default TrackerMark;
