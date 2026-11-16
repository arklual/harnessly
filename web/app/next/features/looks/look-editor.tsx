// The editor of a look: a field of colour where one to three dots are the theme's hues (the angle is the hue, the
// distance from the middle its strength), the light or the dark, a wavy slider of how much the hues tint, a knob of the
// grain, the background, the details and the parts of the chat. Every move repaints the app at once (the look layer);
// the person's settings are written when the move rests (store.ts). Used by Settings → Оформление (for all chats) and
// by the sheet of a folder or a chat (look-sheet.tsx).
import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { sendBlob } from '@/next/core/net/http';
import { clusterApi } from '@/next/core/net/machine';
import { MAX_FX, uploadFx } from '@/next/features/looks/fx-frame';
import { colorOf, gradientOf, rgbOf } from '@/next/features/looks/tokens';
import {
  BUBBLES,
  COMPOSERS,
  FONTS,
  PRESETS,
  RADII,
  SCENES,
  FX_PRESETS,
  type TFxPreset,
  SIDEBARS,
  TURNS,
  type IHue,
  type ILook,
  type TBgKind,
  type TLookMode,
  type TScene,
} from '@/next/features/looks/types';
import { SegControl } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { toast } from '@/next/ui/toast';
import { useCssVars } from '@/next/ui/use-css-vars';

const clamp = (x: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, x));
const round = (x: number) => Math.round(x);

/** A hue's dot on the field: where it stands (angle and distance) and its colour. */
const Dot = ({
  hue,
  index,
  onDown,
  onRemove,
}: {
  hue: IHue;
  index: number;
  onDown: (index: number, event: PointerEvent<HTMLButtonElement>) => void;
  onRemove: (index: number) => void;
}) => {
  const rad = ((hue.h - 90) * Math.PI) / 180;
  const r = (hue.s / 100) * 46;
  const ref = useCssVars<HTMLButtonElement>({
    '--hx-lk-x': `${50 + Math.cos(rad) * r}%`,
    '--hx-lk-y': `${50 + Math.sin(rad) * r}%`,
    '--hx-lk-c': rgbOf(0.7, 0.04 + 0.16 * Math.min(1, hue.s / 70), hue.h),
  });
  return (
    <button
      ref={ref}
      type="button"
      className="hx-lk-dot"
      data-first={index === 0 || undefined}
      aria-label={t('next.looks.dot', { n: index + 1 })}
      onPointerDown={(event) => onDown(index, event)}
      onDoubleClick={(event) => {
        event.stopPropagation();
        onRemove(index);
      }}
    />
  );
};

const DotField = ({ hues, onChange }: { hues: readonly IHue[]; onChange: (hues: IHue[]) => void }) => {
  const field = useRef<HTMLDivElement>(null);
  const drag = useRef<number | null>(null);
  const hueAt = (x: number, y: number): IHue | null => {
    const box = field.current?.getBoundingClientRect();
    if (!box) return null;
    const dx = x - (box.left + box.width / 2);
    const dy = y - (box.top + box.height / 2);
    const h = (((Math.atan2(dy, dx) * 180) / Math.PI + 90 + 360) % 360) | 0;
    const s = round(clamp(Math.hypot(dx, dy) / (box.width * 0.46), 0, 1) * 100);
    return { h, s };
  };
  const move = (event: PointerEvent<HTMLDivElement>) => {
    if (drag.current === null) return;
    const hue = hueAt(event.clientX, event.clientY);
    if (!hue) return;
    onChange(hues.map((old, i) => (i === drag.current ? hue : old)));
  };
  return (
    <div
      ref={field}
      className="hx-lk-field"
      role="group"
      aria-label={t('next.looks.field')}
      onPointerMove={move}
      onPointerUp={() => (drag.current = null)}
      onPointerCancel={() => (drag.current = null)}
      onClick={(event) => {
        if (event.target !== field.current || hues.length >= 3) return;
        const hue = hueAt(event.clientX, event.clientY);
        if (hue) onChange([...hues, hue]);
      }}
    >
      <span className="hx-lk-field__dots" aria-hidden="true" />
      {hues.map((hue, i) => (
        <Dot
          key={i}
          hue={hue}
          index={i}
          onDown={(index, event) => {
            drag.current = index;
            field.current?.setPointerCapture(event.pointerId);
          }}
          onRemove={(index) => hues.length > 1 && onChange(hues.filter((_, k) => k !== index))}
        />
      ))}
    </div>
  );
};

/** The strength of the tint: a range drawn as a wave that fills as it grows. */
const WaveSlider = ({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
}) => {
  const ref = useCssVars<HTMLInputElement>({ '--hx-lk-p': `${value}%` });
  return (
    <label className="hx-lk-wave">
      <span className="hx-lk-label">{label}</span>
      <input
        ref={ref}
        type="range"
        min={0}
        max={100}
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </label>
  );
};

/** The grain: a round knob, turned by a drag up or down (or the arrows). */
const Knob = ({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
}) => {
  const start = useRef<{ y: number; v: number } | null>(null);
  const ref = useCssVars<HTMLButtonElement>({ '--hx-lk-a': `${-135 + value * 2.7}deg` });
  const key = (event: KeyboardEvent<HTMLButtonElement>) => {
    const step = event.shiftKey ? 10 : 2;
    if (event.key === 'ArrowUp' || event.key === 'ArrowRight') onChange(clamp(value + step, 0, 100));
    else if (event.key === 'ArrowDown' || event.key === 'ArrowLeft') onChange(clamp(value - step, 0, 100));
    else return;
    event.preventDefault();
  };
  return (
    <div className="hx-lk-knobwrap">
      <button
        ref={ref}
        type="button"
        className="hx-lk-knob"
        role="slider"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={value}
        onKeyDown={key}
        onPointerDown={(event) => {
          start.current = { y: event.clientY, v: value };
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          if (!start.current) return;
          onChange(round(clamp(start.current.v + (start.current.y - event.clientY) * 0.6, 0, 100)));
        }}
        onPointerUp={() => (start.current = null)}
        onPointerCancel={() => (start.current = null)}
      >
        <span className="hx-lk-knob__tick" aria-hidden="true" />
      </button>
      <span className="hx-lk-label">{label}</span>
    </div>
  );
};

const MODE_ICON = { auto: 'sparkles', light: 'sun', dark: 'moon' } as const satisfies Record<
  TLookMode,
  string
>;

/** A ready look's card: its colours as a swatch, its name. */
const PresetCard = ({
  look,
  name,
  on,
  onPick,
}: {
  look: ILook;
  name: string;
  on: boolean;
  onPick: () => void;
}) => {
  const mode = look.mode === 'light' ? 'light' : 'dark';
  const ref = useCssVars<HTMLSpanElement>({
    '--hx-lk-sw-bg':
      look.bg.kind === 'color'
        ? colorOf(look, mode)
        : rgbOf(mode === 'dark' ? 0.16 : 0.96, 0.02, look.hues[0].h),
    '--hx-lk-sw-fill': gradientOf(look, mode),
  });
  return (
    <button type="button" className="hx-lk-preset" role="radio" aria-checked={on} onClick={onPick}>
      <span ref={ref} className="hx-lk-preset__sw" data-scene={look.bg.scene} aria-hidden="true" />
      <span className="hx-lk-preset__name">{name}</span>
    </button>
  );
};

export const PresetGallery = ({
  current,
  onPick,
}: {
  current: ILook | null;
  onPick: (look: ILook) => void;
}) => (
  <div className="hx-lk-presets" role="radiogroup" aria-label={t('next.looks.presets')}>
    {PRESETS.map((preset) => (
      <PresetCard
        key={preset.id}
        look={preset.look}
        name={t(`next.looks.preset.${preset.id}`)}
        on={current?.name === preset.look.name && current?.bg.scene === preset.look.bg.scene}
        onPick={() => onPick({ ...preset.look, name: t(`next.looks.preset.${preset.id}`) })}
      />
    ))}
  </div>
);

/** A picture made small enough for the settings (2560 px at most, WebP), then kept by its hash. */
const uploadPicture = async (file: File): Promise<string | null> => {
  const bitmap = await createImageBitmap(file);
  const scale = Math.min(1, 2560 / Math.max(bitmap.width, bitmap.height));
  const canvas = document.createElement('canvas');
  canvas.width = Math.round(bitmap.width * scale);
  canvas.height = Math.round(bitmap.height * scale);
  canvas.getContext('2d')?.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
  bitmap.close();
  const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, 'image/webp', 0.86));
  if (!blob) return null;
  const res = await sendBlob(
    clusterApi('/me/looks'),
    blob,
    (v) =>
      typeof v === 'object' && v !== null && typeof (v as { hash?: unknown }).hash === 'string'
        ? (v as { hash: string }).hash
        : null,
    30000,
  );
  return 'ok' in res ? res.value : null;
};

type TFxPick = 'none' | TFxPreset | 'own';
const FX_ORDER: readonly TFxPick[] = ['none', ...FX_PRESETS, 'own'];

const BG_ORDER: readonly TBgKind[] = ['none', 'gradient', 'color', 'image', 'scene'];

export interface ILookEditorProps {
  look: ILook;
  onChange: (look: ILook) => void;
}

const LookEditor = ({ look, onChange }: ILookEditorProps) => {
  const [busy, setBusy] = useState(false);
  const file = useRef<HTMLInputElement>(null);
  const set = (patch: Partial<ILook>) => onChange({ ...look, ...patch, name: undefined });
  const setBg = (patch: Partial<ILook['bg']>) => set({ bg: { ...look.bg, ...patch } });
  const setParts = (patch: Partial<ILook['parts']>) => set({ parts: { ...look.parts, ...patch } });
  const fxFile = useRef<HTMLInputElement>(null);
  const setFx = (fx: ILook['fx']) => onChange({ ...look, fx, name: undefined });
  const fxPick: TFxPick = look.fx?.preset ?? (look.fx?.script ? 'own' : 'none');
  const pickFx = async (f: File | undefined) => {
    if (!f) return;
    setBusy(true);
    const hash = f.size <= MAX_FX ? await uploadFx(await f.text()).catch(() => null) : null;
    setBusy(false);
    if (hash) setFx({ script: hash, power: look.fx?.power, back: look.fx?.back });
    else toast(t('next.looks.fx.uploadFailed'), { tone: 'error' });
  };
  const pickFile = async (f: File | undefined) => {
    if (!f) return;
    setBusy(true);
    const hash = await uploadPicture(f).catch(() => null);
    setBusy(false);
    if (hash) setBg({ kind: 'image', image: hash, fit: 'cover' });
    else toast(t('next.looks.uploadFailed'), { tone: 'error' });
  };
  return (
    <div className="hx-lk">
      <div className="hx-lk-top">
        <DotField hues={look.hues} onChange={(hues) => set({ hues })} />
        <div className="hx-lk-side">
          <div className="hx-lk-modes" role="radiogroup" aria-label={t('next.looks.mode')}>
            {(['auto', 'light', 'dark'] as const).map((mode) => (
              <button
                key={mode}
                type="button"
                role="radio"
                aria-checked={look.mode === mode}
                aria-label={t(`next.looks.mode.${mode}`)}
                title={t(`next.looks.mode.${mode}`)}
                onClick={() => set({ mode })}
              >
                <Icon name={MODE_ICON[mode]} size="sm" />
              </button>
            ))}
          </div>
          <WaveSlider
            label={t('next.looks.intensity')}
            value={look.intensity}
            onChange={(intensity) => set({ intensity })}
          />
          <Knob label={t('next.looks.grain')} value={look.grain} onChange={(grain) => set({ grain })} />
          <p className="hx-lk-hint">{t('next.looks.fieldHint')}</p>
        </div>
      </div>

      <div className="hx-lk-row">
        <span className="hx-lk-label">{t('next.looks.bg')}</span>
        <SegControl
          label={t('next.looks.bg')}
          value={look.bg.kind}
          choices={BG_ORDER.map((value) => ({ value, label: t(`next.looks.bg.${value}`) }))}
          onPick={(kind) => {
            if (kind === 'image') file.current?.click();
            else if (kind === 'scene') setBg({ kind, scene: look.bg.scene ?? 'stars' });
            else setBg({ kind, image: undefined, scene: undefined });
          }}
        />
        <input
          ref={file}
          type="file"
          accept="image/png,image/jpeg,image/webp,image/avif"
          hidden
          onChange={(event) => void pickFile(event.target.files?.[0])}
        />
      </div>
      {look.bg.kind === 'image' && (
        <div className="hx-lk-row">
          <button
            type="button"
            className="hx-btn"
            data-size="sm"
            disabled={busy}
            onClick={() => file.current?.click()}
          >
            <Icon name="image" size="sm" />
            {busy ? t('next.looks.uploading') : t('next.looks.replaceImage')}
          </button>
          <SegControl
            label={t('next.looks.fit')}
            value={look.bg.fit ?? 'cover'}
            choices={[
              { value: 'cover', label: t('next.looks.fit.cover') },
              { value: 'tile', label: t('next.looks.fit.tile') },
            ]}
            onPick={(fit) => setBg({ fit })}
          />
        </div>
      )}
      {look.bg.kind === 'scene' && (
        <>
          <div className="hx-lk-scenes" role="radiogroup" aria-label={t('next.looks.scene')}>
            {SCENES.map((scene: TScene) => (
              <button
                key={scene}
                type="button"
                role="radio"
                aria-checked={look.bg.scene === scene}
                className="hx-lk-chip"
                onClick={() => setBg({ scene })}
              >
                {t(`next.looks.scene.${scene}`)}
              </button>
            ))}
          </div>
          <WaveSlider
            label={t('next.looks.speed')}
            value={look.bg.speed ?? 50}
            onChange={(speed) => setBg({ speed })}
          />
        </>
      )}
      {look.bg.kind !== 'none' && (
        <div className="hx-lk-pair">
          <WaveSlider label={t('next.looks.dim')} value={look.dim} onChange={(dim) => set({ dim })} />
          <WaveSlider
            label={t('next.looks.blur')}
            value={round((look.blur / 24) * 100)}
            onChange={(v) => set({ blur: round((v / 100) * 24) })}
          />
        </div>
      )}

      <div className="hx-lk-row">
        <span className="hx-lk-label">{t('next.looks.fx')}</span>
        <SegControl
          label={t('next.looks.fx')}
          value={fxPick}
          choices={FX_ORDER.map((value) => ({ value, label: t(`next.looks.fx.${value}`) }))}
          onPick={(pick) => {
            if (pick === 'own') fxFile.current?.click();
            else if (pick === 'none') setFx(undefined);
            else setFx({ preset: pick, power: look.fx?.power, back: look.fx?.back });
          }}
        />
        <input
          ref={fxFile}
          type="file"
          accept=".js,text/javascript"
          hidden
          onChange={(event) => void pickFx(event.target.files?.[0])}
        />
      </div>
      {look.fx && (
        <>
          {fxPick !== 'none' && <p className="hx-lk-hint">{t(`next.looks.fx.about.${fxPick}`)}</p>}
          <WaveSlider
            label={t('next.looks.fx.power')}
            value={look.fx.power ?? 60}
            onChange={(power) => setFx({ ...look.fx, power })}
          />
          {fxPick === 'own' && (
            <div className="hx-lk-row">
              <button
                type="button"
                className="hx-btn"
                data-size="sm"
                disabled={busy}
                onClick={() => fxFile.current?.click()}
              >
                {busy ? t('next.looks.uploading') : t('next.looks.fx.replace')}
              </button>
              <SegControl
                label={t('next.looks.fx.layer')}
                value={look.fx.back ? 'back' : 'front'}
                choices={[
                  { value: 'front', label: t('next.looks.fx.layer.front') },
                  { value: 'back', label: t('next.looks.fx.layer.back') },
                ]}
                onPick={(layer) => setFx({ ...look.fx, back: layer === 'back' || undefined })}
              />
            </div>
          )}
        </>
      )}

      <details className="hx-lk-more">
        <summary>{t('next.looks.more')}</summary>
        <div className="hx-lk-grid">
          <span className="hx-lk-label">{t('next.looks.radius')}</span>
          <SegControl
            label={t('next.looks.radius')}
            value={look.radius ?? 'default'}
            choices={RADII.map((value) => ({ value, label: t(`next.looks.radius.${value}`) }))}
            onPick={(radius) => set({ radius: radius === 'default' ? undefined : radius })}
          />
          <span className="hx-lk-label">{t('next.looks.font')}</span>
          <SegControl
            label={t('next.looks.font')}
            value={look.font ?? 'inter'}
            choices={FONTS.map((value) => ({ value, label: t(`next.looks.font.${value}`) }))}
            onPick={(font) => set({ font: font === 'inter' ? undefined : font })}
          />
          <span className="hx-lk-label">{t('next.looks.parts.bubble')}</span>
          <SegControl
            label={t('next.looks.parts.bubble')}
            value={look.parts.bubble ?? 'soft'}
            choices={BUBBLES.map((value) => ({ value, label: t(`next.looks.variant.${value}`) }))}
            onPick={(bubble) => setParts({ bubble: bubble === 'soft' ? undefined : bubble })}
          />
          <span className="hx-lk-label">{t('next.looks.parts.composer')}</span>
          <SegControl
            label={t('next.looks.parts.composer')}
            value={look.parts.composer ?? 'solid'}
            choices={COMPOSERS.map((value) => ({ value, label: t(`next.looks.variant.${value}`) }))}
            onPick={(composer) => setParts({ composer: composer === 'solid' ? undefined : composer })}
          />
          <span className="hx-lk-label">{t('next.looks.parts.turns')}</span>
          <SegControl
            label={t('next.looks.parts.turns')}
            value={look.parts.turns ?? 'plain'}
            choices={TURNS.map((value) => ({ value, label: t(`next.looks.variant.${value}`) }))}
            onPick={(turns) => setParts({ turns: turns === 'plain' ? undefined : turns })}
          />
          <span className="hx-lk-label">{t('next.looks.parts.sidebar')}</span>
          <SegControl
            label={t('next.looks.parts.sidebar')}
            value={look.parts.sidebar ?? 'plain'}
            choices={SIDEBARS.map((value) => ({ value, label: t(`next.looks.variant.${value}`) }))}
            onPick={(sidebar) => setParts({ sidebar: sidebar === 'plain' ? undefined : sidebar })}
          />
        </div>
      </details>
    </div>
  );
};

export default LookEditor;
