// The task an app gave the agent in the person's name (internal/apptask): an inbox that handed a tracker
// card to a new chat. It is the app's card in the feed, never a bubble of words the person typed.
import { memo } from 'react';
import { PeerBody } from '@/next/features/chat/peer-card';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/peers.css';

export interface IAppTask {
  app: string;
  label: string;
  text: string;
}

const ENVELOPE = /^\s*<app-task app="([^"]*)" label="([^"]*)">\n([\s\S]*)\n<\/app-task>\s*$/;
const ZWSP = '​';

const unescape = (s: string) =>
  s
    .replace(/&#34;|&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&');

/** The app's task when the message is one envelope of it, else null. */
export const parseAppTask = (text: string): IAppTask | null => {
  const m = ENVELOPE.exec(text);
  if (!m) return null;
  const app = unescape(m[1] ?? '');
  if (!app) return null;
  const body = (m[3] ?? '')
    .split(`<app-task${ZWSP}`)
    .join('<app-task')
    .split(`</app-task${ZWSP}`)
    .join('</app-task');
  return { app, label: unescape(m[2] ?? '') || app, text: body };
};

/** The app's task: who set it, and its words. */
export const AppTaskCard = memo(({ task }: { task: IAppTask }) => (
  <div className="hx-msg" data-kind="peer">
    <div className="hx-peer-msg" data-delivery="read">
      <div className="hx-peer-msg__head">
        <Icon name="app-window" size="sm" className="hx-peer-msg__ic" />
        <span className="hx-peer-msg__who">{t('next.appTask.from', { app: task.label })}</span>
      </div>
      <PeerBody text={task.text} />
    </div>
  </div>
));
