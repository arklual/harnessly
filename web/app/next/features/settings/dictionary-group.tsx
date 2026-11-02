// «Мои слова»: the words dictation gets wrong, each with the ways it is heard. The list is saved whole, on the page's
// answer (pessimistic): the form keeps what was typed until the runtime has it, and says why when it did not.
import { useId, useState } from 'react';
import {
  heardText,
  parseHeard,
  withoutWord,
  withWord,
  wordProblem,
  type WordProblem,
} from '@/next/features/settings/dictionary';
import { FormError, Group, Row, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { VoiceWord } from '@/next/stores/me';

interface IDictionaryGroupProps {
  words: readonly VoiceWord[];
  /** Saves the whole list; resolves with the reason in words, null when the runtime kept it. */
  onSave: (words: VoiceWord[]) => Promise<string | null>;
}

export const DictionaryGroup = ({ words, onSave }: IDictionaryGroupProps) => {
  const ids = useId();
  const [word, setWord] = useState('');
  const [heard, setHeard] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<WordProblem | null>(null);
  const [refusal, setRefusal] = useState<string | null>(null);

  const reset = () => {
    setWord('');
    setHeard('');
    setEditing(null);
    setProblem(null);
    setRefusal(null);
  };

  const run = async (next: VoiceWord[]): Promise<boolean> => {
    setBusy(true);
    setRefusal(null);
    const reason = await onSave(next);
    setBusy(false);
    setRefusal(reason);
    return reason === null;
  };

  const submit = async () => {
    const entry: VoiceWord = { word: word.trim(), heard: parseHeard(heard) };
    const found = wordProblem(words, entry, editing);
    setProblem(found);
    if (found !== null) return;
    if (await run(withWord(words, entry, editing))) reset();
  };

  const edit = (entry: VoiceWord) => {
    setWord(entry.word);
    setHeard(heardText(entry.heard));
    setEditing(entry.word);
    setProblem(null);
    setRefusal(null);
  };

  const remove = async (entry: VoiceWord) => {
    if ((await run(withoutWord(words, entry.word))) && editing !== null) reset();
  };

  const text = problem === null ? refusal : t(`next.settings.voice.words.problem.${problem}`);
  return (
    <Group title={t('next.settings.voice.words')} foot={t('next.settings.voice.words.foot')}>
      {words.map((entry) => (
        <Row
          key={entry.word}
          type="link"
          label={entry.word}
          hint={entry.heard.length > 0 ? heardText(entry.heard) : t('next.settings.voice.words.auto')}
          busy={busy}
        >
          <div className="hx-voice-words__actions">
            <SetButton tone="ghost" disabled={busy} onClick={() => edit(entry)}>
              {t('next.settings.voice.words.edit')}
            </SetButton>
            <SetButton tone="danger" disabled={busy} onClick={() => void remove(entry)}>
              {t('next.settings.voice.words.remove')}
            </SetButton>
          </div>
        </Row>
      ))}
      <form
        className="hx-key-form hx-voice-words"
        data-state={busy ? 'busy' : undefined}
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <label className="hx-voice-words__field" htmlFor={`${ids}-word`}>
          <span className="hx-nb__label">{t('next.settings.voice.words.word')}</span>
          <input
            id={`${ids}-word`}
            className="hx-set__input"
            value={word}
            autoComplete="off"
            spellCheck={false}
            placeholder="Sinopia"
            aria-invalid={problem !== null && problem !== 'full' ? true : undefined}
            onChange={(e) => {
              setWord(e.target.value);
              setProblem(null);
            }}
          />
        </label>
        <label className="hx-voice-words__field" htmlFor={`${ids}-heard`}>
          <span className="hx-nb__label">{t('next.settings.voice.words.heard')}</span>
          <input
            id={`${ids}-heard`}
            className="hx-set__input"
            value={heard}
            autoComplete="off"
            spellCheck={false}
            placeholder={t('next.settings.voice.words.heard.placeholder')}
            onChange={(e) => {
              setHeard(e.target.value);
              setProblem(null);
            }}
          />
        </label>
        {text && <FormError>{text}</FormError>}
        <div className="hx-key-form__row">
          <SetButton
            tone="primary"
            busy={busy}
            disabled={busy || word.trim() === ''}
            onClick={() => void submit()}
          >
            {editing === null ? t('next.settings.voice.words.add') : t('next.settings.voice.words.save')}
          </SetButton>
          {editing !== null && (
            <SetButton tone="ghost" disabled={busy} onClick={reset}>
              {t('next.settings.voice.words.cancel')}
            </SetButton>
          )}
        </div>
      </form>
    </Group>
  );
};

export default DictionaryGroup;
