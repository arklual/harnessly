import { useEffect, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { useDelayed as useDelayedFlag } from '@/next/ui/skeleton';
import Spinner from '@/next/ui/spinner';
import LookBackdrop from '@/next/features/looks/backdrop';
import LookFx from '@/next/features/looks/fx-layer';

export interface IChatStageProps {
  /** The chat's feed and composer from the chat feature; null while the chat is not there yet. */
  children: ReactNode | null;
  /** The address names a chat the list does not have, after waiting for it. */
  missing?: { onHome: () => void } | null;
  /** The pane is open beside the feed (desktop); the pane itself stands on the window's body (pane-drawer.tsx). */
  paneOpen: boolean;
  /** «В фоне» at the top right of the stage, while it is open. */
  side?: ReactNode;
  /** Another chat was picked: this one's thread fades up before it goes. */
  leaving?: boolean;
  /** The chat opens from the start page: the field comes down from where the greeting held it. */
  fromHome?: boolean;
  /** The chat on the stage: its feed and composer are drawn anew for another chat, and play their entrance again. The
   *  stage itself and the right column (`side`) stay: the column changes its content in place (background-panel.tsx)
   *  instead of vanishing with the stage and growing back from nothing. */
  chatKey?: string | null;
}

/** The chat is not in memory yet: a ring and a line in the middle, as the feed shows them (feed-status.tsx), after 150 ms. */
const FeedLoading = () => {
  const shown = useDelayedFlag(true);
  if (!shown) return null;
  return (
    <div className="hx-feed__loading" role="status" data-busy="true">
      <Spinner />
      <span>{t('next.chatStage.loading')}</span>
    </div>
  );
};

/** The open chat's stage (`/c/:id`): the chat feature draws into it; the right pane sits at its side. */
/** The first moments of a chat on the stage, while its messages rise; rows a scroll mounts later do not replay it. */
const ENTER_MS = 900;

const ChatStage = ({
  children,
  paneOpen,
  side,
  missing,
  leaving,
  fromHome,
  chatKey = null,
}: IChatStageProps) => {
  const [entering, setEntering] = useState(true);
  // Another chat comes in: its messages rise again (state adjusted during render, not in an effect).
  const [enteredKey, setEnteredKey] = useState(chatKey);
  if (enteredKey !== chatKey) {
    setEnteredKey(chatKey);
    setEntering(true);
  }
  useEffect(() => {
    if (!entering) return;
    const timer = setTimeout(() => setEntering(false), ENTER_MS);
    return () => clearTimeout(timer);
  }, [entering, enteredKey]);
  return (
    <div
      className="hx-stage"
      data-phase="chat"
      data-pane={paneOpen || undefined}
      data-leaving={leaving || undefined}
      data-entering={entering || undefined}
      data-arrive={fromHome ? 'home' : undefined}
      data-side={side ? 'true' : undefined}
    >
      <LookBackdrop />
      <LookFx layer="back" />
      {missing && (
        <div className="hx-chatmiss" role="status">
          <b>{t('next.chatStage.missing')}</b>
          <span>{t('next.chatStage.missingNote')}</span>
          <button type="button" className="hx-btn" data-size="sm" onClick={missing.onHome}>
            {t('next.chatStage.toHome')}
          </button>
        </div>
      )}
      {/* The chats kept hidden (bindings/shell.tsx) stay mounted under a missing chat's note: not keyed by the chat. */}
      {children ??
        (!missing && (
          <div className="hx-feed" key={chatKey ?? ''}>
            <div className="hx-feed__inner">
              <FeedLoading />
            </div>
          </div>
        ))}
      {/* A look's live effect over the feed and the field: it never takes the pointer (fx-layer.tsx). */}
      <LookFx layer="front" />
      {side}
    </div>
  );
};

export default ChatStage;
