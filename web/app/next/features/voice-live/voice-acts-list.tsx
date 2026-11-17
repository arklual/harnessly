// The voice's actions as lines under the mascot (voice-acts.ts): what it opened, made, switched, sent, and which agent
// it waits for. A line about a chat opens that chat (the full-screen talk folds to its bar, the talk goes on).
import { goTo, ROUTE } from '@/next/routes/routes';
import { setVoiceExpanded } from '@/next/features/voice-live/field';
import { ACT, useVoiceActs, type IVoiceAct } from '@/next/features/voice-live/voice-acts';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import '@/next/styles/voice-cards.css';

const ICON: Record<IVoiceAct['state'], string> = {
  [ACT.doing]: 'loader-circle',
  [ACT.waiting]: 'clock',
  [ACT.done]: 'check',
  [ACT.failed]: 'x',
};

const Line = ({ act }: { act: IVoiceAct }) => {
  const body = (
    <>
      <Icon name={ICON[act.state]} size="sm" />
      <span className="hx-vacts__text">{act.text}</span>
      {act.state === ACT.waiting && <span className="hx-vacts__state">{t('next.voice.acts.waiting')}</span>}
      {act.state === ACT.failed && <span className="hx-vacts__state">{t('next.voice.acts.failed')}</span>}
    </>
  );
  if (!act.chat)
    return (
      <li className="hx-vacts__row" data-state={act.state}>
        {body}
      </li>
    );
  const chat = act.chat;
  return (
    <li>
      <button
        type="button"
        className="hx-vacts__row"
        data-state={act.state}
        onClick={() => {
          goTo({ kind: ROUTE.chat, id: chat });
          setVoiceExpanded(false);
        }}
      >
        {body}
      </button>
    </li>
  );
};

/** The last few lines (the full-screen talk); `last` shows only the newest (the bar). */
const VoiceActs = ({ last = false }: { last?: boolean }) => {
  const acts = useVoiceActs();
  const shown = last ? acts.slice(-1) : acts.slice(-4);
  if (shown.length === 0) return null;
  return (
    <ul
      className="hx-vacts"
      data-last={last || undefined}
      aria-label={t('next.voice.acts.title')}
      data-nodrag
    >
      {shown.map((a) => (
        <Line key={a.id} act={a} />
      ))}
    </ul>
  );
};

export default VoiceActs;
