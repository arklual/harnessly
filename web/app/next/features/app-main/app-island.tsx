// The chat when it is only a field at the foot of the app: the agent's last line over it (it opens the column), and
// «Стоп» while the agent works. The words go to the same chat as the full field's, as a message.
import { useLayoutEffect, useRef, useState } from 'react';
import { RoundButton } from '@/next/ui/button';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export interface IAppIslandProps {
  /** The agent's last line; null when it has said nothing. */
  last: string | null;
  /** The agent is starting or working on a turn. */
  busy: boolean;
  onSend: (text: string) => void;
  onStop: () => void;
  onExpand: () => void;
}

const FIELD_MAX = 132;

const AppIsland = ({ last, busy, onSend, onStop, onExpand }: IAppIslandProps) => {
  const [text, setText] = useState('');
  const input = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.removeProperty('height');
    if (text) el.style.setProperty('height', `${Math.min(FIELD_MAX, el.scrollHeight)}px`);
  }, [text]);
  const send = () => {
    const words = text.trim();
    if (words === '') return;
    onSend(words);
    setText('');
  };
  return (
    <>
      {(busy || last) && (
        <button type="button" className="hx-appisland__last" onClick={onExpand}>
          {busy ? t('next.appmain.working') : last}
        </button>
      )}
      <div className="hx-appisland__box">
        <textarea
          ref={input}
          className="hx-appisland__input"
          rows={1}
          placeholder={t('next.appmain.ask')}
          aria-label={t('next.appmain.ask')}
          value={text}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              send();
            }
          }}
        />
        {busy && <RoundButton icon="square" label={t('next.appmain.stop')} size="sm" onClick={onStop} />}
        <RoundButton
          icon="arrow-up"
          label={t('next.appmain.send')}
          variant="accent"
          size="sm"
          disabled={text.trim() === ''}
          onClick={send}
        />
      </div>
    </>
  );
};

export default AppIsland;
