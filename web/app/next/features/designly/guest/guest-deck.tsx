// The page of a deck's guest link: the deck as slides, full screen or the recording (deck/deck-guest.tsx), the guest's
// comments over the link's gateway, the name asked once, and what the guest looked at reported when the owner counts it.
import { useEffect, useMemo, useRef, useState } from 'react';
import DeckGuest, {
  type IDeckGuestComments,
  type IDeckGuestSlide,
} from '@/next/features/designly/deck/deck-guest';
import { startOpens, statsOn } from '@/next/features/designly/deck/deck-link-view';
import { createSeenTracker, sendSeen } from '@/next/features/designly/deck/guest-seen';
import { chaptersOf } from '@/next/features/designly/deck/video-chapters';
import { boardUrl, type IGuestConfig } from '@/next/features/designly/guest/guest-config';
import {
  keepWho,
  postComment,
  readWho,
  type IGuestAnchor,
} from '@/next/features/designly/guest/guest-comments';
import GuestNameDialog from '@/next/features/designly/guest/guest-name-dialog';
import { useComments } from '@/next/features/designly/guest/use-guest-comments';
import { useGuestPdf } from '@/next/features/designly/guest/use-guest-pdf';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

const clock = (): number => Date.now();
const BOARD_W = 1920;
const BOARD_H = 1080;

/** A deck comment's block, as far as the page knows it: the label of what was under the place (the owner reads it as the chip). */
const anchorOf = (label: string, x: number, y: number, w: number, h: number): IGuestAnchor | null =>
  label === ''
    ? null
    : {
        selector: 'body',
        dataId: null,
        tag: 'div',
        label: label.slice(0, 120),
        rect: { x: (x / 100) * w, y: (y / 100) * h, width: 0, height: 0 },
        state: {},
      };

const GuestDeck = ({ config }: { config: IGuestConfig }) => {
  const [who, setWho] = useState(readWho);
  const [naming, setNaming] = useState(false);
  const asked = useRef<((named: boolean) => void) | null>(null);
  const [list, setList] = useComments(config);

  const slides = useMemo<IDeckGuestSlide[]>(
    () =>
      config.boards.map((b) => ({
        id: b.id,
        title: b.title,
        path: b.path,
        src: boardUrl(config.filesBase, b.path),
        w: b.w ?? BOARD_W,
        h: b.h ?? BOARD_H,
      })),
    [config.boards, config.filesBase],
  );

  // The recording and the PDF are there when the runtime published them and the owner handed them out in the link's view.
  const published = config.deck;
  const hasVideo = published?.video != null && config.view?.video === true;
  const video = hasVideo
    ? {
        src: `${config.mediaBase}video.mp4`,
        chapters: chaptersOf(published?.video?.chapters ?? [], config.boards),
      }
    : null;
  const pdf = useGuestPdf(config);

  const commenting = config.rights === 'comment' && config.commentsUrl !== '';
  const comments: IDeckGuestComments | null = commenting
    ? {
        ensureName: () =>
          who.name !== ''
            ? Promise.resolve(true)
            : new Promise<boolean>((resolve) => {
                asked.current = resolve;
                setNaming(true);
              }),
        pins: list.flatMap((c) => {
          const slide = slides.findIndex((s) => s.path === c.board);
          return slide < 0 || c.point === null
            ? []
            : [{ id: c.id, slide, x: c.point.x * 100, y: c.point.y * 100 }];
        }),
        send: async (slide, at, label, text) => {
          const s = slides[slide];
          try {
            const taken = await postComment(config.commentsUrl, {
              who,
              text,
              board: s.path,
              anchor: anchorOf(label, at.x, at.y, s.w, s.h),
              point: { x: at.x / 100, y: at.y / 100 },
            });
            setList((all) => [...all, taken]);
            toast(t('next.guest.sent'), { tone: 'success' });
            return true;
          } catch {
            toast(t('next.guest.failed'), { tone: 'error' });
            return false;
          }
        },
      }
    : null;

  // What the guest looked at: the board on screen while the tab is visible.
  const counting = statsOn(config.view);
  const tracker = useMemo(
    () =>
      createSeenTracker(clock, (report) => {
        if (counting) sendSeen(config.seenUrl, who.guestId, report);
      }),
    [counting, config.seenUrl, who.guestId],
  );
  const current = useRef<string | null>(null);
  useEffect(() => {
    const sync = () => tracker.show(document.visibilityState === 'visible' ? current.current : null);
    const leave = () => tracker.show(null);
    document.addEventListener('visibilitychange', sync);
    window.addEventListener('pagehide', leave);
    return () => {
      document.removeEventListener('visibilitychange', sync);
      window.removeEventListener('pagehide', leave);
      tracker.show(null);
    };
  }, [tracker]);

  const settle = (named: boolean) => {
    asked.current?.(named);
    asked.current = null;
  };

  return (
    <>
      <DeckGuest
        title={config.title}
        slides={slides}
        video={video}
        pdf={pdf}
        start={startOpens(config.view, video !== null)}
        comments={comments}
        onShown={(path) => {
          current.current = path;
          tracker.show(document.visibilityState === 'visible' ? path : null);
        }}
      />
      {commenting && (
        <GuestNameDialog
          key={naming ? 'open' : 'shut'}
          open={naming}
          name={who.name}
          onClose={() => {
            setNaming(false);
            settle(false);
          }}
          onSave={(name) => {
            const next = { ...who, name };
            keepWho(next);
            setWho(next);
            setNaming(false);
            settle(true);
          }}
        />
      )}
    </>
  );
};

export default GuestDeck;
