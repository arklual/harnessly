// The clips of a 3D scene: a draft animation of the camera on screen, the clips made (draft and final, with where
// they came from), «Add to video», and the final render on a rented GPU — a plan with its price first, a render only after the
// person confirms it. The GPU is never rented by anything but the button in the plan.
import { useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { goTo, ROUTE } from '@/next/routes/routes';
import type { MachineId } from '@/next/protocol/v2';
import {
  clipToVideo,
  clipUrl,
  estimateGeneration,
  outputUrl,
  planFinal,
  requestClip,
  requestOutputs,
  startFinal,
  startGeneration,
  type IGenParams,
} from '@/next/features/designly/scene3d/api';
import { clock, timeOf } from '@/next/features/designly/scene3d/scene-model';
import type {
  IScene3dCam,
  IScene3dClip,
  IScene3dFinal,
  IScene3dFinalPlan,
  IScene3dGeneration,
  IScene3dOutputs,
  IScene3dMeta,
  IScene3dVideoResult,
} from '@/next/features/designly/scene3d/types';

export interface IClipsProps {
  machine: MachineId;
  id: string;
  meta: IScene3dMeta;
  clips: readonly IScene3dClip[];
  finals: readonly IScene3dFinal[];
  /** The view on screen: a camera of the scene by name, or the free view. */
  view: () => IScene3dCam | null;
  canEdit: boolean;
  onError: (message: string) => void;
  outputs: readonly IScene3dOutputs[];
  generations: readonly IScene3dGeneration[];
  /** The machine has a video generator (a provider): «Send to a generator» is offered. */
  generator: boolean;
  /** The mesh the person picked: a mask of it is made with the outputs. */
  selected: string;
}

const camLabel = (cam: IScene3dCam) => cam.n ?? t('next.scene3d.camFree');

const Clips = ({
  machine,
  id,
  meta,
  clips,
  finals,
  view,
  canEdit,
  onError,
  outputs,
  generations,
  generator,
  selected,
}: IClipsProps) => {
  const [busy, setBusy] = useState(false);
  const [plan, setPlan] = useState<IScene3dFinalPlan | null>(null);
  const [agree, setAgree] = useState(false);
  const [sent, setSent] = useState<Readonly<Record<string, IScene3dVideoResult>>>({});

  const fail = (e: unknown) => onError(e instanceof Error ? e.message : String(e));
  const camBody = (): { n: string } | IScene3dCam | null => {
    const v = view();
    return v ? (v.n ? { n: v.n } : v) : null;
  };

  const draft = async () => {
    const cam = camBody();
    if (!cam || busy) return;
    setBusy(true);
    try {
      await requestClip(machine, id, cam, meta.start, meta.end);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const askPlan = async () => {
    const cam = camBody();
    if (!cam || busy) return;
    setBusy(true);
    setAgree(false);
    try {
      setPlan(await planFinal(machine, id, cam, meta.start, meta.end));
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const rent = async () => {
    if (!plan || !agree || busy) return;
    setBusy(true);
    try {
      await startFinal(machine, id, plan.id);
      setPlan(null);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const makeOutputs = async () => {
    const cam = camBody();
    if (!cam || busy) return;
    setBusy(true);
    try {
      const mesh = meta.objects.find((o) => o.n === selected && o.t === 'MESH');
      await requestOutputs(machine, id, cam, [], mesh ? [mesh.n] : []);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const toVideo = async (clip: IScene3dClip) => {
    setBusy(true);
    try {
      const res = await clipToVideo(machine, id, clip.key);
      setSent((cur) => ({ ...cur, [clip.key]: res }));
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hx-scene3d__clips">
      {canEdit && (
        <div className="hx-scene3d__clipbar">
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={busy}
            onClick={() => void draft()}
          >
            <Icon name="film" size="sm" />
            {t('next.scene3d.clipDraft')}
          </button>
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={busy}
            onClick={() => void askPlan()}
          >
            <Icon name="rocket" size="sm" />
            {t('next.scene3d.finalPlan')}
          </button>
        </div>
      )}

      {canEdit && (
        <div className="hx-scene3d__clipbar">
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={busy}
            onClick={() => void makeOutputs()}
          >
            <Icon name="layers" size="sm" />
            {t('next.scene3d.outputsMake')}
          </button>
        </div>
      )}

      {plan && (
        <section className="hx-scene3d__plan" aria-label={t('next.scene3d.finalTitle')}>
          <h3>{t('next.scene3d.finalTitle')}</h3>
          <p>
            {t('next.scene3d.finalWhat', {
              cam: camLabel(plan.spec.cam),
              frames: plan.frames,
              seconds: plan.seconds.toFixed(1),
            })}
          </p>
          <p className="hx-scene3d__price">
            {t('next.scene3d.finalPrice', {
              rub: Math.round(plan.estimateRub).toLocaleString(),
              hours: plan.hours.toFixed(1),
            })}
          </p>
          {plan.warnings.length > 0 && <p className="hx-scene3d__warn">{plan.warnings.join(' · ')}</p>}
          {plan.dry && <pre className="hx-scene3d__error">{plan.dry}</pre>}
          <label className="hx-scene3d__agree">
            <input type="checkbox" checked={agree} onChange={(e) => setAgree(e.target.checked)} />
            {t('next.scene3d.finalAgree', { rub: Math.round(plan.estimateRub).toLocaleString() })}
          </label>
          <div className="hx-scene3d__composerRow">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              onClick={() => setPlan(null)}
            >
              {t('next.scene3d.cancel')}
            </button>
            <button
              type="button"
              className="hx-btn"
              data-variant="primary"
              data-size="sm"
              disabled={!agree || busy}
              onClick={() => void rent()}
            >
              {t('next.scene3d.finalRent')}
            </button>
          </div>
        </section>
      )}

      {finals.map((f) => (
        <p key={f.id} className="hx-scene3d__final" data-status={f.status}>
          {t('next.scene3d.finalJob', {
            cam: camLabel(f.Plan.spec.cam),
            phase: f.status === 'building' ? f.phase : f.status,
          })}
          {f.error ? ` — ${f.error}` : ''}
        </p>
      ))}

      <ul className="hx-scene3d__outputs">
        {outputs.map((o) => (
          <OutputsCard
            key={o.key}
            machine={machine}
            id={id}
            o={o}
            generator={generator}
            canEdit={canEdit}
            generations={generations.filter((g) => g.outputs === o.key)}
            onError={onError}
          />
        ))}
      </ul>

      {clips.length === 0 && finals.length === 0 && (
        <p className="hx-scene3d__empty">{t('next.scene3d.clipsEmpty')}</p>
      )}
      <ul className="hx-scene3d__clipList">
        {clips.map((c) => {
          const put = sent[c.key];
          return (
            <li key={c.key} data-status={c.status}>
              {c.status === 'ready' ? (
                <video src={clipUrl(machine, id, c.key)} controls preload="metadata" muted playsInline />
              ) : (
                <p className="hx-scene3d__frameStatus">
                  {c.status === 'failed'
                    ? (c.error ?? t('next.scene3d.frameFailed'))
                    : t('next.scene3d.clipProgress', { done: c.done, total: c.total })}
                </p>
              )}
              <p className="hx-scene3d__framemeta">
                {c.origin.kind === 'final'
                  ? t('next.scene3d.kindFinal')
                  : c.origin.kind === 'generated'
                    ? t('next.scene3d.kindGenerated')
                    : t('next.scene3d.kindDraft')}{' '}
                · {camLabel(c.origin.cam)} · {clock(timeOf(meta, c.origin.start))}–
                {clock(timeOf(meta, c.origin.end))}
                {c.origin.step > 1 ? ` · ${t('next.scene3d.everyNth', { n: c.origin.step })}` : ''}
                {c.origin.where ? ` · ${c.origin.where}` : ''}
              </p>
              {c.status === 'ready' && canEdit && !put && (
                <button
                  type="button"
                  className="hx-btn"
                  data-variant="ghost"
                  data-size="sm"
                  disabled={busy}
                  onClick={() => void toVideo(c)}
                >
                  <Icon name="clapperboard" size="sm" />
                  {t('next.scene3d.toVideo')}
                </button>
              )}
              {put && (
                <button
                  type="button"
                  className="hx-btn"
                  data-variant="ghost"
                  data-size="sm"
                  onClick={() => goTo({ kind: ROUTE.site, id: put.video, machine })}
                >
                  {t('next.scene3d.openVideo')}
                </button>
              )}
            </li>
          );
        })}
      </ul>
    </div>
  );
};

interface IOutputsCardProps {
  machine: MachineId;
  id: string;
  o: IScene3dOutputs;
  generator: boolean;
  canEdit: boolean;
  generations: readonly IScene3dGeneration[];
  onError: (message: string) => void;
}

/** One set of outputs: its key frames' pictures, the manifest, and «Send to a generator» with a price before anything is spent. */
const OutputsCard = ({ machine, id, o, generator, canEdit, generations, onError }: IOutputsCardProps) => {
  const [open, setOpen] = useState(false);
  const [params, setParams] = useState<IGenParams>({
    outputs: o.key,
    model: 'kling',
    prompt: '',
    seconds: 5,
    source: 'beauty',
  });
  const [price, setPrice] = useState<{ usd: number; note: string } | null>(null);
  const [agree, setAgree] = useState(false);
  const [busy, setBusy] = useState(false);
  const fail = (e: unknown) => onError(e instanceof Error ? e.message : String(e));
  const m = o.manifest;
  const first = m?.keyframes[0];
  const change = (patch: Partial<IGenParams>) => {
    setParams((cur) => ({ ...cur, ...patch }));
    setPrice(null);
    setAgree(false);
  };
  const estimate = async () => {
    setBusy(true);
    try {
      setPrice(await estimateGeneration(machine, id, params));
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  const generate = async () => {
    setBusy(true);
    try {
      await startGeneration(machine, id, params);
      setOpen(false);
      setPrice(null);
      setAgree(false);
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };
  if (o.status !== 'ready' || !m || !first)
    return (
      <li className="hx-scene3d__outset" data-status={o.status}>
        <p className="hx-scene3d__frameStatus">
          {o.status === 'failed' ? (o.error ?? t('next.scene3d.frameFailed')) : t('next.scene3d.frameBusy')}
        </p>
      </li>
    );
  const pics = [
    ['beauty', first.beauty],
    ['clay', first.clay],
    ['depth', first.depth],
    ['normals', first.normals],
  ].filter((p): p is [string, string] => typeof p[1] === 'string');
  return (
    <li className="hx-scene3d__outset" data-status="ready">
      <p className="hx-scene3d__framemeta">
        {t('next.scene3d.outputsSet', {
          cam: camLabel(m.camera),
          frames: m.keyframes.length,
          masks: m.masks.length,
        })}
      </p>
      <div className="hx-scene3d__outpics">
        {pics.map(([kind, name]) => (
          <figure key={kind}>
            <img src={outputUrl(machine, id, o.key, name)} alt={kind} loading="lazy" />
            <figcaption>{kind}</figcaption>
          </figure>
        ))}
      </div>
      <div className="hx-scene3d__clipbar">
        <a
          className="hx-btn"
          data-variant="ghost"
          data-size="sm"
          href={outputUrl(machine, id, o.key, 'manifest.json')}
          target="_blank"
          rel="noreferrer"
        >
          {t('next.scene3d.manifest')}
        </a>
        {canEdit && (
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            disabled={!generator}
            title={generator ? undefined : t('next.scene3d.noGenerator')}
            onClick={() => setOpen((v) => !v)}
          >
            <Icon name="sparkles" size="sm" />
            {t('next.scene3d.generate')}
          </button>
        )}
      </div>
      {generations.map((g) => (
        <p key={g.id} className="hx-scene3d__final" data-status={g.state === 'failed' ? 'failed' : g.state}>
          {t('next.scene3d.genJob', { model: g.model, state: g.clip ? t('next.scene3d.genTaken') : g.state })}
          {g.error ? ` — ${g.error}` : ''}
        </p>
      ))}
      {open && (
        <section className="hx-scene3d__plan" aria-label={t('next.scene3d.generate')}>
          <p>{t('next.scene3d.genWhat')}</p>
          <label className="hx-scene3d__select">
            <select
              value={params.model}
              onChange={(e) => change({ model: e.target.value })}
              aria-label={t('next.scene3d.genModel')}
            >
              <option value="kling">Kling</option>
              <option value="seedance">Seedance</option>
              <option value="ltx">LTX</option>
            </select>
            <select
              value={params.source}
              onChange={(e) => change({ source: e.target.value as IGenParams['source'] })}
              aria-label={t('next.scene3d.genSource')}
            >
              <option value="beauty">beauty</option>
              <option value="clay">clay</option>
            </select>
            <select
              value={params.seconds}
              onChange={(e) => change({ seconds: Number(e.target.value) })}
              aria-label={t('next.scene3d.genSeconds')}
            >
              {[3, 5, 8, 10].map((s) => (
                <option key={s} value={s}>
                  {s} s
                </option>
              ))}
            </select>
          </label>
          <textarea
            rows={3}
            value={params.prompt}
            placeholder={t('next.scene3d.genPrompt')}
            onChange={(e) => change({ prompt: e.target.value })}
          />
          <div className="hx-scene3d__composerRow">
            <button
              type="button"
              className="hx-btn"
              data-variant="ghost"
              data-size="sm"
              disabled={busy || !params.prompt.trim()}
              onClick={() => void estimate()}
            >
              {t('next.scene3d.genPrice')}
            </button>
          </div>
          {price && (
            <>
              <p className="hx-scene3d__price">{t('next.scene3d.genCost', { usd: price.usd.toFixed(2) })}</p>
              <label className="hx-scene3d__agree">
                <input type="checkbox" checked={agree} onChange={(e) => setAgree(e.target.checked)} />
                {t('next.scene3d.genAgree', { usd: price.usd.toFixed(2) })}
              </label>
              <div className="hx-scene3d__composerRow">
                <button
                  type="button"
                  className="hx-btn"
                  data-variant="primary"
                  data-size="sm"
                  disabled={!agree || busy}
                  onClick={() => void generate()}
                >
                  {t('next.scene3d.genStart')}
                </button>
              </div>
            </>
          )}
        </section>
      )}
    </li>
  );
};

export default Clips;
