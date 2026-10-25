import type { ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';

/** How long the project chip stays after it is cleared, leaving. */
const INTO_EXIT_MS = 200;

export interface IGreetingProps {
  /** The person's name; without it the question goes without one. */
  name: string | null;
  onResume?: () => void;
  /** The composer, from the composer feature. */
  composer?: ReactNode;
  /** The project the new chat starts in, when one was picked; the cross clears it. */
  into?: { name: string; onClear: () => void } | null;
  /** The places to open, under the field. */
  tools?: ReactNode;
  /** The page opens from a chat: its field rises up under the greeting; from any other screen it stands in place. */
  fromChat?: boolean;
}

/** The start page (`/`): the question in the middle of the stage, the way to continue a terminal session, the field. */
const Greeting = ({ name, onResume, composer, into = null, tools, fromChat = false }: IGreetingProps) => {
  const chip = usePresence(into, INTO_EXIT_MS);
  return (
    <div
      className="hx-stage hx-enter"
      data-enter="fade"
      data-eb="200"
      data-ed="700"
      data-phase="home"
      data-back={fromChat ? 'chat' : undefined}
    >
      <div className="hx-hello">
        <div className="hx-enter" data-enter="up" data-eb="420">
          <div className="hx-hello__title">
            {name ? t('next.greeting.title', { name }) : t('next.greeting.titleNoName')}
          </div>
          <div className="hx-hello__sub">{t('next.greeting.sub')}</div>
          {chip.value && (
            <div className="hx-hello__into" data-state={chip.state}>
              <Icon name="folder" size="sm" />
              <span className="hx-hello__into-name">
                {t('next.newChat.inProject', { project: chip.value.name })}
              </span>
              <button
                type="button"
                className="hx-hello__into-x"
                aria-label={t('next.newChat.inProjectClear')}
                title={t('next.newChat.inProjectClear')}
                onClick={chip.value.onClear}
              >
                <Icon name="x" size="sm" />
              </button>
            </div>
          )}
          {onResume && (
            <button type="button" className="hx-hello__resume" onClick={onResume}>
              <Icon name="history" size="sm" />
              {t('next.greeting.resume')}
            </button>
          )}
        </div>
      </div>
      {composer}
      {tools}
    </div>
  );
};

export default Greeting;
