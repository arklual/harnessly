// One preset of a vendor: the key form while it has no key (or is being replaced), else the saved key as its last
// digits with «Проверить подключение», «Заменить ключ» and «Удалить ключ». What the check found is said in a plain line
// under it. The panel never holds a saved key, only the one being typed.
import { useEffect, useRef, useState } from 'react';
import {
  cleanKey,
  isKeyShape,
  type IPresetActions,
  type IPresetState,
  type TestFailure,
  type TestState,
} from '@/next/features/providers/model';
import { FormError, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import SecretField from '@/next/ui/secret-field';

/** «Точно удалить?» waits this long for the second press. */
const SURE_MS = 4000;

/** What each reason says, in the words of what the person does about it. */
const TEST_TEXT = {
  'no-key': 'next.providers.test.noKey',
  unauthorized: 'next.providers.test.unauthorized',
  forbidden: 'next.providers.test.forbidden',
  'rate-limited': 'next.providers.test.rateLimited',
  unreachable: 'next.providers.test.unreachable',
  timeout: 'next.providers.test.timeout',
  rejected: 'next.providers.test.rejected',
  page: 'next.providers.test.page',
} as const satisfies Record<TestFailure, string>;

const testLine = (test: TestState): string | null => {
  if (test.k === 'ok') return t('next.providers.testOk');
  return test.k === 'failed' ? t(TEST_TEXT[test.reason]) : null;
};

interface IPresetPanelProps extends IPresetActions {
  state: IPresetState;
  /** The vendor's name, for the field's label. */
  vendor: string;
  /** Called once a key has been saved (the first-run step moves on from it). */
  onSaved?: () => void;
}

const PresetPanel = ({ state, vendor, onSave, onRemove, onTest, onSaved }: IPresetPanelProps) => {
  const [replacing, setReplacing] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [sure, setSure] = useState(false);
  const [test, setTest] = useState<TestState>({ k: 'idle' });
  const field = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const configured = state.key.configured;
  const form = !configured || replacing;

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  // The form takes the cursor when it opens over a saved key.
  useEffect(() => {
    if (replacing) field.current?.focus();
  }, [replacing]);
  // A second press has four seconds; then the button is as it was.
  useEffect(() => {
    if (!sure) return;
    const timer = window.setTimeout(() => setSure(false), SURE_MS);
    return () => window.clearTimeout(timer);
  }, [sure]);

  const stopReplacing = () => {
    setReplacing(false);
    setDraft('');
    setError(null);
  };
  const save = async () => {
    const key = cleanKey(draft);
    if (!isKeyShape(key)) {
      setError(t('next.providers.keyShort'));
      return;
    }
    setError(null);
    setSaving(true);
    const refused = await onSave(state.id, key);
    if (!alive.current) return;
    setSaving(false);
    if (refused !== null) {
      setError(refused);
      return;
    }
    // The key is out of the page the moment the machine has it.
    setDraft('');
    setReplacing(false);
    setTest({ k: 'idle' });
    onSaved?.();
  };
  const check = async () => {
    setTest({ k: 'testing' });
    const result = await onTest(state.id);
    if (alive.current) setTest(result);
  };
  const remove = async () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    setRemoving(true);
    const refused = await onRemove(state.id);
    if (!alive.current) return;
    setRemoving(false);
    if (refused !== null) setError(refused);
    else setTest({ k: 'idle' });
  };

  const line = testLine(test);
  // The lines stay on screen while their rows close.
  const [kept, setKept] = useState({ text: '', ok: false });
  const [keptError, setKeptError] = useState('');
  const ok = test.k === 'ok';
  if (line !== null && (kept.text !== line || kept.ok !== ok)) setKept({ text: line, ok });
  if (error !== null && keptError !== error) setKeptError(error);
  return (
    <div className="hx-preset">
      {state.hint !== '' && <p className="hx-preset__hint">{state.hint}</p>}
      <div className="hx-reveal" data-open={form}>
        <div className="hx-reveal__in" inert={!form || undefined}>
          <div className="hx-preset__form" data-state={saving ? 'busy' : undefined}>
            <SecretField
              ref={field}
              value={draft}
              onChange={(value) => {
                setDraft(value);
                setError(null);
              }}
              label={t('next.providers.keyAria', { vendor })}
              placeholder={t('next.providers.keyPlaceholder')}
              disabled={saving}
              invalid={error !== null}
              onSubmit={() => void save()}
            />
            <div className="hx-preset__row">
              <SetButton
                tone="primary"
                busy={saving}
                disabled={draft.trim() === ''}
                onClick={() => void save()}
              >
                {t('next.providers.connect')}
              </SetButton>
              {configured && (
                <SetButton tone="ghost" onClick={stopReplacing}>
                  {t('next.providers.cancel')}
                </SetButton>
              )}
            </div>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={!form}>
        <div className="hx-reveal__in" inert={form || undefined}>
          <div className="hx-preset__saved">
            <Icon name="circle-check" size="sm" />
            <span>{t('next.providers.keySaved', { tail: state.key.tail ?? '' })}</span>
          </div>
          <div className="hx-preset__row">
            <SetButton tone="primary" busy={test.k === 'testing'} onClick={() => void check()}>
              {test.k === 'testing' ? t('next.providers.testing') : t('next.providers.test')}
            </SetButton>
            <SetButton tone="ghost" onClick={() => setReplacing(true)}>
              {t('next.providers.replace')}
            </SetButton>
            <SetButton tone="ghost" busy={removing} onClick={() => void remove()}>
              {sure ? t('next.providers.removeSure') : t('next.providers.remove')}
            </SetButton>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={line !== null && !form}>
        <div className="hx-reveal__in">
          <div className="hx-preset__test" data-ok={kept.ok || undefined} role="status">
            <Icon name={kept.ok ? 'circle-check' : 'triangle-alert'} size="sm" />
            <span>{kept.text}</span>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={error !== null}>
        <div className="hx-reveal__in">
          <FormError>{keptError}</FormError>
        </div>
      </div>
    </div>
  );
};

export default PresetPanel;
