// The talk away from its chat (harnessly design project → «Голос — в другом чате»): a compact bar at the top
// centre over any screen but the chat itself. «Говорите в чате «…»» with the voice's head and small wave · Вернуться ·
// a message (a field opens in the bar; Enter sends it to the same chat, the talk goes on; Esc folds it) · Завершить.
// A narrow window keeps the head, the chat's name and two round buttons: ← back, × end.
import { useEffect, useRef, useState } from 'react';
import { sendMessage } from '@/next/commands';
import { sendMessage as sendGpt } from '@/next/features/chatgpt/api';
import { GPT_VOICE_EVENT, gptIdOf, gptPickOf, tellGptScreen } from '@/next/features/chatgpt/voice-bridge';
import VoiceHead, { HEAD, headOf } from '@/next/features/voice-live/voice-head';
import MiniWave from '@/next/features/voice-live/mini-wave';
import { lookOf, useVoiceNow, type IVoiceField } from '@/next/features/voice-live/field';
import { useVoiceDocked } from '@/next/features/voice-live/voice-layer';
import { t } from '@/next/i18n';
import type { ConvId } from '@/next/protocol/v2';
import { RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import '@/next/styles/voice-layer.css';

/** The bar leaves in this long (hx-voice-bar-out). */
const EXIT_MS = 200;

interface IVoiceBarProps {
  /** The chat's name, null while the list does not know it. */
  titleOf: (conv: ConvId) => string | null;
  /** «Вернуться»: the talk's chat opens. */
  onBack: (conv: ConvId) => void;
  /** Where the person is (the route, as a key): the bar checks again whether the talk's field is on screen. */
  at: string;
}

const VoiceBar = ({ titleOf, onBack, at }: IVoiceBarProps) => {
  const now = useVoiceNow();
  const docked = useVoiceDocked(now?.conv ?? null, at);
  const away = now?.conv && !docked ? now : null;
  const { value: voice, state } = usePresence<IVoiceField>(away, EXIT_MS);
  if (!voice?.conv) return null;
  return (
    <VoiceBarBody
      voice={voice}
      title={titleOf(voice.conv) ?? t('next.chatgpt.voice.bar.untitled')}
      closing={state === PRESENCE.closing}
      onBack={onBack}
    />
  );
};

const VoiceBarBody = ({
  voice,
  title,
  closing,
  onBack,
}: {
  voice: IVoiceField;
  title: string;
  closing: boolean;
  onBack: (conv: ConvId) => void;
}) => {
  const conv = voice.conv as ConvId;
  const [writing, setWriting] = useState(false);
  const [text, setText] = useState('');
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (writing) input.current?.focus();
  }, [writing]);
  const look = lookOf(voice.floor);
  const head = voice.interrupted ? HEAD.barged : headOf(voice.floor);
  const send = () => {
    const words = text.trim();
    if (!words) return;
    // A ChatGPT chat's talk: the words go to that chat, with what its screen picked (voice-bridge.ts).
    const gpt = gptIdOf(conv);
    if (gpt === null) {
      sendMessage(voice.machine, { conversationId: conv, text: words, attachments: [], mode: 'queue' });
    } else {
      const pick = gptPickOf(gpt);
      void sendGpt(voice.machine, gpt, {
        text: words,
        model: pick.model,
        effort: pick.effort,
        files: [],
      }).then(
        () => tellGptScreen(gpt, GPT_VOICE_EVENT.sent),
        () => undefined,
      );
    }
    setText('');
    setWriting(false);
  };
  return (
    <div className="hx-voice-bar" data-s={look} data-closing={closing || undefined} role="status">
      <div className="hx-voice-bar__row">
        <VoiceHead state={head} kind={voice.head} level={voice.level} />
        <span className="hx-voice-bar__t">
          <span className="hx-voice-bar__wide">{t('next.chatgpt.voice.bar.in')} </span>
          <b>«{title}»</b>
        </span>
        <MiniWave look={look} />
        <span className="hx-voice-bar__sep hx-voice-bar__wide" />
        <button type="button" className="hx-voice-bar__b hx-voice-bar__wide" onClick={() => onBack(conv)}>
          {t('next.chatgpt.voice.bar.back')}
        </button>
        <button
          type="button"
          className="hx-voice-bar__i hx-voice-bar__wide"
          aria-label={t('next.chatgpt.voice.bar.message')}
          title={t('next.chatgpt.voice.bar.message')}
          aria-pressed={writing}
          data-on={writing || undefined}
          onClick={() => setWriting((was) => !was)}
        >
          <Icon name="message-square" size="sm" />
        </button>
        <button
          type="button"
          className="hx-voice-bar__b hx-voice-bar__b--end hx-voice-bar__wide"
          onClick={() => voice.end()}
        >
          {t('next.chatgpt.voice.bar.end')}
        </button>
        <button
          type="button"
          className="hx-voice-bar__i hx-voice-bar__narrow"
          aria-label={t('next.chatgpt.voice.bar.back')}
          onClick={() => onBack(conv)}
        >
          <Icon name="arrow-left" size="sm" />
        </button>
        <button
          type="button"
          className="hx-voice-bar__i hx-voice-bar__i--end hx-voice-bar__narrow"
          aria-label={t('next.chatgpt.voice.bar.end')}
          onClick={() => voice.end()}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {writing && (
        <form
          className="hx-voice-bar__m hx-voice-bar__wide"
          onSubmit={(event) => {
            event.preventDefault();
            send();
          }}
        >
          <input
            ref={input}
            className="hx-voice-bar__in"
            value={text}
            placeholder={t('next.chatgpt.voice.bar.placeholder', { title })}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== 'Escape') return;
              // Esc folds the field; the talk goes on (the voice's own Esc would end it).
              event.preventDefault();
              event.stopPropagation();
              event.nativeEvent.stopImmediatePropagation();
              setWriting(false);
            }}
          />
          <RoundButton
            icon="arrow-up"
            variant="accent"
            size="sm"
            onClick={send}
            label={t('next.chatgpt.voice.bar.send')}
            disabled={!text.trim()}
          />
        </form>
      )}
    </div>
  );
};

export default VoiceBar;
