// A talk about one item of the day's digest (harnessly design project → «Главная — дайджест голосом»): one
// line under the summary, not a card — the voice's head, the person's words dimmed, the agent's reply lit as it is said
// (the said part bright, the rest grey) and «Открыть пункт». With «Только голос» nothing is written: the head and the
// wave. The item talked about is outlined on the page by `DIGEST_ITEM` with `data-digest-voice="true"`.
import { RecordingWave } from '@/next/features/composer/recording';
import VoiceHead, { type THead } from '@/next/features/voice-live/voice-head';
import { t } from '@/next/i18n';
import { useVoiceMascot } from '@/next/stores/me';
import '@/next/styles/voice-layer.css';

/** The class of an item of the digest (a priority): outlined while the voice talks about it (`data-digest-voice`). */
export const DIGEST_ITEM = 'hx-digest-item';

interface IDigestLineProps {
  head: THead;
  /** What the person said, as heard; empty while they have not. */
  me?: string;
  /** The agent's reply: the part said so far, then the rest. */
  said?: string;
  rest?: string;
  /** «Открыть пункт»: the item talked about opens. */
  onOpen?: () => void;
  openLabel?: string;
  /** «Только голос»: no words, the wave of whoever speaks. */
  voiceOnly?: boolean;
  level?: (() => number) | null;
}

const DigestLine = ({
  head,
  me = '',
  said = '',
  rest = '',
  onOpen,
  openLabel,
  voiceOnly = false,
  level = null,
}: IDigestLineProps) => {
  const kind = useVoiceMascot();
  return (
    <div className="hx-digest-line">
      <VoiceHead state={head} kind={kind} level={level} />
      {voiceOnly ? (
        <RecordingWave level={level} listening />
      ) : (
        <div className="hx-digest-line__b">
          {me && (
            <span className="hx-digest-line__me">{t('next.chatgpt.voice.digest.you', { text: me })}</span>
          )}
          {(said || rest) && (
            <span className="hx-digest-line__say">
              <b>{said}</b>
              {rest && ` ${rest}`}
              {onOpen && openLabel && (
                <>
                  {' '}
                  <button type="button" className="hx-digest-line__open" onClick={onOpen}>
                    {openLabel}
                  </button>
                </>
              )}
            </span>
          )}
        </div>
      )}
    </div>
  );
};

export default DigestLine;
