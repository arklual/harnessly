// The tab of a change (merge request, pull request, commit, issue): the «Ревью кода» app's viewer, in the chat's pane
// (`usePaneContents`), in the start page's pane (bindings/home-pane.tsx) and in the gallery (next/dev/change-gallery.tsx).
// A narrow pane folds the file tree and shows the diff in one column (container queries of review-app.css); the
// button «Открыть в «Ревью кода»» opens the same change on the whole screen.
import { useCallback } from 'react';
import '@/styles/legacy';
import { changeTitleOf, gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { MrStage } from '@/next/legacy/review-stages';
import { sendMessage } from '@/next/commands';
import type { ConvId } from '@/next/protocol/v2';
import { useConversation } from '@/next/stores/conversations';
import { goTo, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';

const SHA_LENGTH = 8;

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

const openFull = (url: string, chat?: ConvId) => goTo({ kind: ROUTE.review, url, ...(chat ? { chat } : {}) });

/** A change in the chat's pane: questions about its lines go to this chat. */
export const ChangePane = ({ url, chatId }: { url: string; chatId: string | null }) => {
  const chat = chatId !== null && isConvId(chatId) ? chatId : null;
  const record = useConversation(chat);
  const machine = record?.machineId;
  const send = useCallback(
    (message: string) => {
      if (chat && machine) sendMessage(machine, { conversationId: chat, text: message });
    },
    [chat, machine],
  );
  const ask = useCallback(
    (text: string) => send(`${t('next.reviewApp.notes.msgQuestion')}\n${url}\n\n${text}`),
    [send, url],
  );
  const beside = chat && machine;
  return (
    <div className="hx-rp-change">
      <MrStage
        url={url}
        ask={beside ? ask : undefined}
        send={beside ? send : undefined}
        chat={beside ? chat : null}
        machine={machine}
        expand={() => openFull(url, chat ?? undefined)}
      />
    </div>
  );
};

export const ChangeTab = ({ url }: { url: string }) => <MrStage url={url} expand={() => openFull(url)} />;

/** A change opened from the review picker of the start page: no chat takes questions on its lines. */
export const ReviewChange = ({ url }: { url: string }) => (
  <div className="hx-rp-change">
    <MrStage url={url} expand={() => openFull(url)} />
  </div>
);

/** What the tab of a link is called, or the link itself when it is not a change. */
export const changeTabName = (url: string): string => {
  const ref = gitlabRefOf(url);
  return ref ? changeTitleOf(ref, SHA_LENGTH) : url;
};
