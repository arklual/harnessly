// What a space (the boards, the documents) looks like before its view is up: its list as quiet tiles, or an
// open board or document as an empty sheet. It stands from the first frame — while the page still reads the machine's
// apps, and on under the view until the view says it is ready — so the screen is never blank and never changes shape.
import '@/next/styles/app-views.css';

const TILES = 6;

interface ISpaceSkeletonProps {
  /** Something open in the space (a board, a document); false: its list. */
  object: boolean;
}

export const SpaceSkeleton = ({ object }: ISpaceSkeletonProps) =>
  object ? (
    <div className="hx-spacewait" data-shape="sheet">
      <span className="hx-spacewait__line" />
      <span className="hx-spacewait__line" />
      <span className="hx-spacewait__sheet" />
    </div>
  ) : (
    <div className="hx-spacewait" data-shape="list">
      <span className="hx-spacewait__line" />
      <div className="hx-spacewait__tiles">
        {Array.from({ length: TILES }, (_, i) => (
          <span key={i} className="hx-spacewait__tile" />
        ))}
      </div>
    </div>
  );

/** The page's own wait for a space, before its view is known: the same skeleton, in the frame's place. */
export const SpacePageWait = ({ object }: ISpaceSkeletonProps) => (
  <div className="hx-appview__box hx-spacewait__page" aria-busy="true">
    <div className="hx-appview__skeleton" data-state="loading">
      <SpaceSkeleton object={object} />
    </div>
  </div>
);
