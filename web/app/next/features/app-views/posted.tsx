// The cards apps posted into this chat (hl.chat.card), at the end of the feed. Each is the app's own `chat` view, drawn
// through the same bridge as a card of a tool step, under the app's name so it is plain who put it there.
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps } from '@/next/features/app-views/api';
import { useAppViewHost } from '@/next/features/app-views/host';
import { usePostedCards } from '@/next/features/app-views/posted-cards';
import '@/next/styles/app-views.css';

const PostedCards = () => {
  const host = useAppViewHost();
  const cards = usePostedCards(host?.conversation ?? null);
  // The installed list is read only once the chat has a card to draw.
  const { list } = useInstalledApps(host?.machine ?? 'local', 0, cards.length > 0);
  if (host === null || list === null) return null;
  return (
    <>
      {cards.map((card) => {
        const app = list.apps.find((a) => a.id === card.app);
        const view = app?.views.find((v) => v.id === card.view && v.targets.includes('chat'));
        if (!app || !view) return null;
        return (
          <div key={card.id} className="hx-appcard" data-posted="true">
            <span className="hx-appcard__from">{app.name}</span>
            <AppFrame
              machine={host.machine}
              app={app}
              view={view}
              target="chat"
              viewPort={list.viewPort}
              conversation={host.conversation}
              data={card.data}
              fit
            />
          </div>
        );
      })}
    </>
  );
};

export default PostedCards;
