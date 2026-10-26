// A key of an agent or a vendor's preset, entered here: a write-only field while it has no key (or is being replaced),
// else the saved key as its last digits with «Проверить подключение» (where the machine can), «Заменить ключ» and «Удалить
// ключ». The field is never filled from outside and is emptied the moment the key is sent; replacing and deleting wait for
// a second press. What the check found is said in a plain line under it. The panel never holds a saved key.
import { useEffect, useRef, useState } from 'react';
import { cleanKey, isKeyShape, type TestFailure, type TestState } from '@/next/features/providers/model';
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

interface IKeyPanelProps {
  /** One line on the key (the plan, where it is from); empty when there is none. */
  hint: string;
  /** What the field is called for a screen reader: «Ключ API Kimi». */
  label: string;
  /** What the machine has: whether a key is saved and its last digits. */
  saved: { configured: boolean; tail: string | null };
  /** The vendor CLI's sign-in the profile runs on while no key is saved: a subscription, checked like a key. */
  login?: { account: string };
  /** Saves the key; resolves null when taken, else the reason. */
  onSave: (key: string) => Promise<string | null>;
  onRemove: () => Promise<string | null>;
  /** The check of the connection, where the machine can make one. */
  onTest?: () => Promise<TestState>;
  /** Called once a key has been saved. */
  onSaved?: () => void;
}

const KeyPanel = ({ hint, label, saved, login, onSave, onRemove, onTest, onSaved }: IKeyPanelProps) => {
  const [replacing, setReplacing] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [sure, setSure] = useState(false);
  const [sureReplace, setSureReplace] = useState(false);
  const [test, setTest] = useState<TestState>({ k: 'idle' });
  const field = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const configured = saved.configured;
  const form = !configured || replacing;
  // Without a key the CLI's sign-in does the work; the form stays to put a key in its place.
  const signed = !configured && login !== undefined;

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
    if (!sure && !sureReplace) return;
    const timer = window.setTimeout(() => {
      setSure(false);
      setSureReplace(false);
    }, SURE_MS);
    return () => window.clearTimeout(timer);
  }, [sure, sureReplace]);

  const stopReplacing = () => {
    setReplacing(false);
    setSureReplace(false);
    setDraft('');
    setError(null);
  };
  const save = async () => {
    const key = cleanKey(draft);
    if (!isKeyShape(key)) {
      setError(t('next.providers.keyShort'));
      return;
    }
    // A key over a saved one is replaced on the second press.
    if (configured && !sureReplace) {
      setSureReplace(true);
      return;
    }
    setError(null);
    setSureReplace(false);
    setSaving(true);
    // The key leaves the page's state the moment it is sent, whatever the answer.
    setDraft('');
    const refused = await onSave(key);
    if (!alive.current) return;
    setSaving(false);
    if (refused !== null) {
      setError(refused);
      return;
    }
    setReplacing(false);
    setTest({ k: 'idle' });
    onSaved?.();
  };
  const check = async () => {
    if (!onTest) return;
    setTest({ k: 'testing' });
    const result = await onTest();
    if (alive.current) setTest(result);
  };
  const remove = async () => {
    if (!sure) {
      setSure(true);
      return;
    }
    setSure(false);
    setRemoving(true);
    const refused = await onRemove();
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
      {hint !== '' && <p className="hx-preset__hint">{hint}</p>}
      {login !== undefined && (
        <div className="hx-reveal" data-open={signed}>
          <div className="hx-reveal__in" inert={!signed || undefined}>
            <div className="hx-preset__saved">
              <Icon name="circle-check" size="sm" />
              <span>{t('next.providers.loginSaved', { account: login?.account ?? '' })}</span>
            </div>
            <p className="hx-preset__where">{t('next.providers.loginNote')}</p>
            {onTest && (
              <div className="hx-preset__row">
                <SetButton tone="primary" busy={test.k === 'testing'} onClick={() => void check()}>
                  {test.k === 'testing' ? t('next.providers.testing') : t('next.providers.test')}
                </SetButton>
              </div>
            )}
          </div>
        </div>
      )}
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
              label={label}
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
                {configured
                  ? t(sureReplace ? 'next.providers.replaceSure' : 'next.providers.replaceDo')
                  : t('next.providers.connect')}
              </SetButton>
              {configured && (
                <SetButton tone="ghost" onClick={stopReplacing}>
                  {t('next.providers.cancel')}
                </SetButton>
              )}
            </div>
            <p className="hx-preset__where">{t('next.providers.where')}</p>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={!form}>
        <div className="hx-reveal__in" inert={form || undefined}>
          <div className="hx-preset__saved">
            <Icon name="circle-check" size="sm" />
            <span>
              {saved.tail
                ? t('next.providers.keySaved', { tail: saved.tail })
                : t('next.providers.keySavedPlain')}
            </span>
          </div>
          <div className="hx-preset__row">
            {onTest && (
              <SetButton tone="primary" busy={test.k === 'testing'} onClick={() => void check()}>
                {test.k === 'testing' ? t('next.providers.testing') : t('next.providers.test')}
              </SetButton>
            )}
            <SetButton tone="ghost" onClick={() => setReplacing(true)}>
              {t('next.providers.replace')}
            </SetButton>
            <SetButton tone="ghost" busy={removing} onClick={() => void remove()}>
              {sure ? t('next.providers.removeSure') : t('next.providers.remove')}
            </SetButton>
          </div>
        </div>
      </div>
      <div className="hx-reveal" data-open={line !== null && (!form || signed)}>
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

export default KeyPanel;
