// A font file as a specimen (.hx-font): the text the person types in it at several sizes, the alphabet and the digits.
// The font is read through the chat and loaded under a name of its own (styles/pane.css names it), so it never
// replaces one of the app's; one specimen is on screen at a time.
import { useEffect, useState } from 'react';
import { t } from '@/next/i18n';
import LoadFailed from '@/next/ui/load-failed';

const SIZES = [12, 16, 24, 36, 56] as const;
const FAMILY = 'hx-specimen';

const FontView = ({ href, name }: { href: string; name: string }) => {
  const [state, setState] = useState<'loading' | 'ready' | string>('loading');
  const [attempt, setAttempt] = useState(0);
  const [sample, setSample] = useState(() => t('next.font.sample'));
  useEffect(() => {
    let alive = true;
    let face: FontFace | null = null;
    fetch(href, { credentials: 'same-origin' })
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.arrayBuffer();
      })
      .then((bytes) => {
        face = new FontFace(FAMILY, bytes);
        return face.load();
      })
      .then((loaded) => {
        if (!alive) return;
        document.fonts.add(loaded);
        setState('ready');
      })
      .catch((failure: unknown) => {
        if (alive) setState(failure instanceof Error ? failure.message : String(failure));
      });
    return () => {
      alive = false;
      if (face) document.fonts.delete(face);
    };
  }, [href, attempt]);
  if (state !== 'loading' && state !== 'ready')
    return (
      <LoadFailed
        title={t('next.font.failed')}
        reason={state}
        onRetry={() => {
          setState('loading');
          setAttempt((n) => n + 1);
        }}
      />
    );
  return (
    <div className="hx-font" data-ready={state === 'ready' || undefined} aria-busy={state === 'loading'}>
      <input
        className="hx-font__field"
        value={sample}
        onChange={(event) => setSample(event.target.value)}
        aria-label={t('next.font.field', { name })}
      />
      {SIZES.map((size) => (
        <div key={size} className="hx-font__line">
          <span className="hx-font__size">{size}</span>
          <span className="hx-font__text" data-size={size}>
            {sample || t('next.font.sample')}
          </span>
        </div>
      ))}
      <div className="hx-font__glyphs">
        <p>ABCDEFGHIJKLMNOPQRSTUVWXYZ</p>
        <p>abcdefghijklmnopqrstuvwxyz</p>
        <p>АБВГДЕЁЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ</p>
        <p>абвгдеёжзийклмнопрстуфхцчшщъыьэюя</p>
        <p>0123456789 ₽ $ € % &amp; @ « » — ! ?</p>
      </div>
    </div>
  );
};

export default FontView;
