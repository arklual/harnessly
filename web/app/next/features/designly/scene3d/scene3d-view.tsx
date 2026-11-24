// The 3D scene's shell: the bar, the stage with the viewer (orbit, the scene's cameras, modes, the frame guide) over the
// timeline (scene3d-timeline.tsx), and the panel on the right that folds away (objects, pins, draft frames, checks). The
// viewer itself (three.js, viewer.ts) is loaded when a model is ready. A pin is an object, a point on its surface, the
// view and the frame; a click on it takes the camera back; its composer is a popover by the point.
import { copyWriter } from '@/next/offline/client';
import { limit as offlineLimit } from '@/next/offline/storage';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { t } from '@/next/i18n';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { BuilderPicker } from '@/next/features/builders/builder-picker';
import Icon from '@/next/ui/icon';
import ModeButton from '@/next/features/designly/mode-button';
import type { ISiteAccess, ISiteMode } from '@/next/features/designly/site-view';
import type { MachineId } from '@/next/protocol/v2';
import {
  fetchScene3d,
  fixUrl,
  frameUrl,
  modelUrl,
  postPin,
  rebuildScene3d,
  reportView,
  requestFrame,
  resolvePin,
  shotUrl,
} from '@/next/features/designly/scene3d/api';
import {
  CHECK,
  checksOf,
  clock,
  pathTo,
  timeOf,
  treeOf,
  type ITreeNode,
} from '@/next/features/designly/scene3d/scene-model';
import {
  FRAME_STATUS,
  MODEL_STATUS,
  VIEW_MODE,
  type IScene3dCam,
  type IScene3dFrame,
  type IScene3dPin,
  type IScene3dState,
  type IScene3dViewCmd,
  type TVec3,
  type TViewMode,
} from '@/next/features/designly/scene3d/types';
import type { SceneViewer } from '@/next/features/designly/scene3d/viewer';
import Clips from '@/next/features/designly/scene3d/scene3d-clips';
import Timeline from '@/next/features/designly/scene3d/scene3d-timeline';
import { putScene3dPin, resolveScene3dPin, setScene3d } from '@/next/stores/scene3d';

export interface IScene3dViewProps {
  machine: MachineId;
  id: string;
  title: string;
  state: IScene3dState | undefined;
  /** Why the state could not be read, null while it is fine. */
  stateError: string | null;
  /** Pins may be placed: the project is edited (or commented on), not only looked at. */
  canPin: boolean;
  mode?: ISiteMode;
  access?: ISiteAccess;
  pane?: { onBack: () => void };
  maxed: boolean;
  onToggleMax: (() => void) | null;
  onOpenDrawer: () => void;
}

const TAB = { objects: 'objects', pins: 'pins', frames: 'frames', checks: 'checks' } as const;
type TTab = (typeof TAB)[keyof typeof TAB];

const POLL_FRAME_MS = 1500;

const FREE = '';

// ---- the tree

interface ITreeProps {
  nodes: readonly ITreeNode[];
  selected: string;
  open: ReadonlySet<string>;
  onToggle: (name: string) => void;
  onPick: (name: string) => void;
  depth?: number;
}

const Tree = ({ nodes, selected, open, onToggle, onPick, depth = 0 }: ITreeProps) => (
  <ul className="hx-scene3d__tree" data-depth={depth}>
    {nodes.map(({ object, children }) => (
      <li key={object.n}>
        <div
          className="hx-scene3d__row"
          data-selected={object.n === selected}
          style={{ paddingInlineStart: 6 + depth * 12 }}
        >
          {children.length > 0 ? (
            <button
              type="button"
              className="hx-scene3d__fold"
              aria-expanded={open.has(object.n)}
              aria-label={object.n}
              onClick={() => onToggle(object.n)}
            >
              <Icon name={open.has(object.n) ? 'chevron-down' : 'chevron-right'} size="sm" />
            </button>
          ) : (
            <span className="hx-scene3d__fold" />
          )}
          <button
            type="button"
            className="hx-scene3d__name"
            data-kind={object.t}
            onClick={() => onPick(object.n)}
          >
            <span className="hx-scene3d__type">{object.t.slice(0, 3)}</span>
            {object.n}
            {object.hidden && <span className="hx-scene3d__dim"> · {t('next.scene3d.hidden')}</span>}
          </button>
        </div>
        {children.length > 0 && open.has(object.n) && (
          <Tree
            nodes={children}
            selected={selected}
            open={open}
            onToggle={onToggle}
            onPick={onPick}
            depth={depth + 1}
          />
        )}
      </li>
    ))}
  </ul>
);

// ---- the view

interface IDraft {
  object?: string;
  p?: TVec3;
  text: string;
}

const Scene3dView = (props: IScene3dViewProps) => {
  const { machine, id, state } = props;
  const meta = state?.model.status === MODEL_STATUS.ready ? (state.model.meta ?? null) : null;
  const hash = state?.model.hash ?? '';
  // The viewer lives as long as the model's hash does: a poll that brings the same model again is not a new model.
  // eslint-disable-next-line react-hooks/exhaustive-deps -- by the hash on purpose
  const stableMeta = useMemo(() => meta, [hash]);
  const host = useRef<HTMLDivElement | null>(null);
  const viewer = useRef<SceneViewer | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [frame, setFrame] = useState(1);
  const [playing, setPlaying] = useState(false);
  const [camera, setCamera] = useState(FREE);
  /** The object the orbit follows while it moves (its Blender name), '' for none. */
  const [following, setFollowing] = useState('');
  const [mode, setMode] = useState<TViewMode>(VIEW_MODE.material);
  const [tab, setTab] = useState<TTab>(TAB.objects);
  const [selected, setSelected] = useState('');
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [draft, setDraft] = useState<IDraft | null>(null);
  const [activePin, setActivePin] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [guide, setGuide] = useState<{ x: number; y: number; w: number; h: number } | null>(null);
  const [shown, setShown] = useState<IScene3dFrame | null>(null);
  const [drafting, setDrafting] = useState(false);
  const [builder, setBuilder] = useState('');
  const pickerBase = useMemo(() => builderBase(runtimePath(platformPath('/builders'), machine)), [machine]);
  const statePins = state?.pins;
  const pins = useMemo(() => statePins ?? [], [statePins]);

  const tree = useMemo(() => (meta ? treeOf(meta.objects) : []), [meta]);
  const checks = useMemo(() => (meta ? checksOf(meta) : []), [meta]);

  // ---- the viewer lives while a model is on screen
  useEffect(() => {
    const meta = stableMeta;
    if (!meta || !hash || !host.current) return;
    let live = true;
    const abort = new AbortController();
    const el = host.current;
    setLoaded(false);
    setLoadError(null);
    void import('@/next/features/designly/scene3d/viewer').then(async ({ SceneViewer: Viewer }) => {
      if (!live) return;
      const v = new Viewer(el, meta, {
        onPick: (object, p) => {
          setSelected(object ?? '');
          v.select(object ?? '');
          if (object && meta)
            setOpen((cur) => new Set([...cur, ...pathTo(meta.objects, object).slice(0, -1)]));
          setDraft((cur) =>
            cur === null && object === null
              ? null
              : { text: cur?.text ?? '', ...(object ? { object } : {}), ...(p ? { p } : {}) },
          );
          setActivePin('');
        },
        onPin: (pid) => {
          setActivePin(pid);
          setTab(TAB.pins);
        },
        onFrame: (f) => setFrame(f),
        onPlaying: setPlaying,
      });
      viewer.current = v;
      try {
        const write = copyWriter();
        await v.load(modelUrl(machine, id, hash), abort.signal, (bytes) => {
          if (write && bytes.byteLength <= offlineLimit)
            void write(
              ['scene', machine, id],
              props.title,
              'model',
              new Blob([bytes], { type: 'model/gltf-binary' }),
            ).catch(() => undefined);
        });
        if (!live) return;
        setFrame(meta.start);
        setLoaded(true);
        setGuide(v.guide());
        v.setMode(VIEW_MODE.material);
      } catch (failure) {
        if (live) setLoadError(failure instanceof Error ? failure.message : String(failure));
      }
    });
    const watch = new ResizeObserver(() => viewer.current && setGuide(viewer.current.guide()));
    watch.observe(el);
    return () => {
      live = false;
      abort.abort();
      watch.disconnect();
      viewer.current?.dispose();
      viewer.current = null;
      setLoaded(false);
    };
  }, [machine, id, hash, stableMeta, props.title]);

  useEffect(() => {
    viewer.current?.setPins(pins, activePin);
  }, [pins, activePin, loaded]);

  // ---- what the agent sees of the viewer and asks of it: one short poll
  const here = useRef({ selected, activePin, playing, frame });
  const act = useRef<(c: IScene3dViewCmd) => void>(() => undefined);
  useEffect(() => {
    here.current = { selected, activePin, playing, frame };
  });
  useEffect(() => {
    if (!loaded) return;
    let live = true;
    let acked = 0;
    const abort = new AbortController();
    const tick = async () => {
      const v = viewer.current;
      if (!v || document.visibilityState !== 'visible') return;
      const cur = here.current;
      try {
        const cmds = await reportView(
          machine,
          id,
          {
            object: cur.selected,
            pin: cur.activePin,
            cam: v.getView(),
            frame: v.currentFrame(),
            playing: cur.playing,
            acked,
          },
          abort.signal,
        );
        for (const c of cmds) {
          if (!live || c.seq <= acked) continue;
          acked = c.seq;
          act.current(c);
        }
      } catch {
        // the poll is a convenience: a failure is the next tick's retry
      }
    };
    void tick();
    const timer = setInterval(() => void tick(), 2500);
    return () => {
      live = false;
      abort.abort();
      clearInterval(timer);
    };
  }, [machine, id, loaded]);

  const lastError = (e: unknown) => setFailure(e instanceof Error ? e.message : String(e));

  // ---- the camera, the modes, the time
  const pickCamera = (name: string) => {
    setCamera(name);
    viewer.current?.useCamera(name);
    setGuide(viewer.current?.guide() ?? null);
  };
  const pickMode = (m: TViewMode) => {
    setMode(m);
    viewer.current?.setMode(m);
  };
  const seek = (f: number) => {
    viewer.current?.pause();
    viewer.current?.seek(f);
    setFrame(f);
  };
  const togglePlay = () => (viewer.current?.isPlaying() ? viewer.current.pause() : viewer.current?.play());

  const pickObject = (name: string) => {
    setSelected(name);
    viewer.current?.select(name);
    setDraft((cur) => ({ text: cur?.text ?? '', object: name }));
  };
  const toggleOpen = (name: string) =>
    setOpen((cur) => {
      const next = new Set(cur);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });

  // ---- the draft frame "as it will be"
  // What the page itself asked for since the last read lies over the runtime's list until the next read has it.
  const [asked, setAsked] = useState<Readonly<Record<string, IScene3dFrame>>>({});
  const stateFrames = state?.frames;
  const frames = useMemo(() => {
    const byKey = new Map<string, IScene3dFrame>();
    for (const f of stateFrames ?? []) byKey.set(f.key, f);
    for (const f of Object.values(asked))
      if (byKey.get(f.key)?.status !== FRAME_STATUS.ready) byKey.set(f.key, f);
    return [...byKey.values()];
  }, [stateFrames, asked]);
  const keepFrame = (f: IScene3dFrame) => setAsked((cur) => ({ ...cur, [f.key]: f }));
  const wanted = useRef<{ key: string } | null>(null);

  const askFrame = useCallback(
    async (cam: IScene3dCam, at: number, retry = false): Promise<IScene3dFrame | null> => {
      const cur = viewer.current;
      if (!cur) return null;
      setDrafting(true);
      setFailure(null);
      try {
        const body = cam.n ? { n: cam.n, pos: cam.pos, target: cam.target, fov: cam.fov } : cam;
        let f = await requestFrame(machine, id, body, at, retry, builder);
        wanted.current = { key: f.key };
        keepFrame(f);
        // The page asks again while the machine draws: the runtime answers from its job, or from the cache when done.
        while (
          wanted.current?.key === f.key &&
          (f.status === FRAME_STATUS.queued || f.status === FRAME_STATUS.building)
        ) {
          await new Promise((r) => setTimeout(r, POLL_FRAME_MS));
          if (wanted.current?.key !== f.key) return null;
          f = await requestFrame(machine, id, body, at, false, builder);
          keepFrame(f);
        }
        if (f.status === FRAME_STATUS.failed) setFailure(f.error ?? t('next.scene3d.frameFailed'));
        else setShown(f);
        return f;
      } catch (e) {
        lastError(e);
        return null;
      } finally {
        setDrafting(false);
      }
    },
    [machine, id, builder],
  );

  const draftHere = () => {
    const v = viewer.current;
    if (v) void askFrame(v.getView(), v.currentFrame());
  };

  // ---- pins
  const savePin = async () => {
    const v = viewer.current;
    if (!v || !draft || !draft.text.trim() || busy) return;
    setBusy(true);
    setFailure(null);
    try {
      const cam = v.getView();
      const at = v.currentFrame();
      const same = frames.find(
        (f) => f.status === FRAME_STATUS.ready && f.frame === at && f.cam && sameView(f.cam, cam),
      );
      const pin = await postPin(machine, id, {
        ...(draft.object ? { object: draft.object } : {}),
        ...(draft.p ? { p: draft.p } : {}),
        cam,
        frame: at,
        text: draft.text.trim(),
        ...(same ? { render: same.key } : {}),
        shot: v.snapshot(),
      });
      putScene3dPin(id, pin);
      setDraft(null);
      setActivePin(pin.id);
      setTab(TAB.pins);
    } catch (e) {
      lastError(e);
    } finally {
      setBusy(false);
    }
  };

  const goToPin = (pin: IScene3dPin) => {
    setActivePin(pin.id);
    const v = viewer.current;
    if (!v) return;
    if (pin.scene.cam) {
      v.goTo(pin.scene.cam, pin.scene.frame);
      setCamera(
        pin.scene.cam.n && meta?.cameras.some((c) => c.n === pin.scene.cam?.n) ? pin.scene.cam.n : FREE,
      );
    } else v.seek(pin.scene.frame);
    setFrame(pin.scene.frame);
    if (pin.scene.object) {
      setSelected(pin.scene.object);
      v.select(pin.scene.object);
    }
    setGuide(v.guide());
  };

  useEffect(() => {
    act.current = (c) => {
      const v = viewer.current;
      if (!v) return;
      const pin = c.pin ? pins.find((p) => p.id === c.pin) : undefined;
      if (pin) {
        goToPin(pin);
        return;
      }
      if (c.cam) {
        v.goTo(c.cam, c.frame);
        setCamera(c.cam.n && meta?.cameras.some((x) => x.n === c.cam?.n) ? c.cam.n : FREE);
        setGuide(v.guide());
      } else if (c.frame !== undefined) seek(c.frame);
      if (c.frame !== undefined) setFrame(c.frame);
      if (c.object) pickObject(c.object);
    };
  });

  const toggleResolved = async (pin: IScene3dPin) => {
    resolveScene3dPin(id, pin.id, !pin.resolved);
    try {
      await resolvePin(machine, id, pin.id, !pin.resolved);
    } catch (e) {
      resolveScene3dPin(id, pin.id, pin.resolved);
      lastError(e);
    }
  };

  const retryBuild = async () => {
    try {
      await rebuildScene3d(machine, id);
      setScene3d(id, await fetchScene3d(machine, id));
    } catch (e) {
      lastError(e);
    }
  };

  // ---- parts
  const model = state?.model;
  const waiting = !state
    ? (props.stateError ?? t('next.scene3d.loading'))
    : model?.status === MODEL_STATUS.building || model?.status === MODEL_STATUS.none
      ? t('next.scene3d.building')
      : null;

  const stage = (): ReactNode => {
    if (waiting)
      return (
        <div className="hx-scene3d__note" role="status">
          {waiting}
        </div>
      );
    if (model?.status === MODEL_STATUS.failed)
      return (
        <div className="hx-scene3d__note" role="alert">
          <p>{t('next.scene3d.failed')}</p>
          {model.error && <pre className="hx-scene3d__error">{model.error}</pre>}
          {state?.blender === false && <p>{t('next.scene3d.noBlender')}</p>}
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            onClick={() => void retryBuild()}
          >
            {t('next.scene3d.retry')}
          </button>
        </div>
      );
    return (
      <>
        <div className="hx-scene3d__host" ref={host} />
        {!loaded && !loadError && (
          <div className="hx-scene3d__note" role="status">
            {t('next.scene3d.loadingModel')}
          </div>
        )}
        {loadError && (
          <div className="hx-scene3d__note" role="alert">
            {loadError}
          </div>
        )}
        {loaded && guide && (
          <div
            className="hx-scene3d__guide"
            aria-hidden="true"
            style={{ left: guide.x, top: guide.y, width: guide.w, height: guide.h }}
          />
        )}
      </>
    );
  };

  const frameOfPin = (pin: IScene3dPin) =>
    pin.scene.render ? frameUrl(machine, id, pin.scene.render) : null;

  const objectTab = meta && (
    <div className="hx-scene3d__scroll">
      <Tree nodes={tree} selected={selected} open={open} onToggle={toggleOpen} onPick={pickObject} />
    </div>
  );

  const pinsTab = (
    <div className="hx-scene3d__scroll">
      {pins.length === 0 && (
        <p className="hx-scene3d__empty">
          {props.canPin ? t('next.scene3d.pinsEmpty') : t('next.scene3d.pinsNone')}
        </p>
      )}
      <ol className="hx-scene3d__pins">
        {pins.map((pin, i) => (
          <li key={pin.id} data-active={pin.id === activePin} data-resolved={pin.resolved}>
            <button type="button" className="hx-scene3d__pin" onClick={() => goToPin(pin)}>
              <span className="hx-scene3d__pinno">{i + 1}</span>
              <span className="hx-scene3d__pinbody">
                <span className="hx-scene3d__pinmeta">
                  {pin.by.name}
                  {pin.scene.object ? ` · ${pin.scene.object}` : ''}
                  {meta ? ` · ${clock(timeOf(meta, pin.scene.frame))}` : ` · #${pin.scene.frame}`}
                </span>
                <span className="hx-scene3d__pintext">{pin.text}</span>
              </span>
            </button>
            {pin.id === activePin && (
              <div className="hx-scene3d__pinextra">
                <div className="hx-scene3d__pics">
                  {pin.scene.shot && (
                    <img src={shotUrl(machine, id, pin.id)} alt={t('next.scene3d.shotAlt')} loading="lazy" />
                  )}
                  {pin.scene.fix?.before && (
                    <figure>
                      <img
                        src={fixUrl(machine, id, pin.id, 'before')}
                        alt={t('next.scene3d.before')}
                        loading="lazy"
                      />
                      <figcaption>{t('next.scene3d.before')}</figcaption>
                    </figure>
                  )}
                  {pin.scene.fix?.after && (
                    <figure>
                      <img
                        src={fixUrl(machine, id, pin.id, 'after')}
                        alt={t('next.scene3d.after')}
                        loading="lazy"
                      />
                      <figcaption>{t('next.scene3d.after')}</figcaption>
                    </figure>
                  )}
                  {!pin.scene.fix && frameOfPin(pin) && (
                    <img
                      src={frameOfPin(pin) ?? undefined}
                      alt={t('next.scene3d.renderAlt')}
                      loading="lazy"
                    />
                  )}
                </div>
                {props.canPin && (
                  <button
                    type="button"
                    className="hx-btn"
                    data-variant="ghost"
                    data-size="sm"
                    onClick={() => void toggleResolved(pin)}
                  >
                    {pin.resolved ? t('next.scene3d.reopen') : t('next.scene3d.resolve')}
                  </button>
                )}
              </div>
            )}
          </li>
        ))}
      </ol>
    </div>
  );

  const framesTab = (
    <div className="hx-scene3d__scroll">
      {meta && (
        <Clips
          machine={machine}
          id={id}
          meta={meta}
          clips={state?.clips ?? []}
          outputs={state?.outputs ?? []}
          generations={state?.generations ?? []}
          generator={state?.generator === true}
          selected={selected}
          finals={state?.finals ?? []}
          view={() => viewer.current?.getView() ?? null}
          canEdit={props.canPin}
          onError={setFailure}
        />
      )}
      {frames.length === 0 && <p className="hx-scene3d__empty">{t('next.scene3d.framesEmpty')}</p>}
      <ul className="hx-scene3d__frames">
        {frames.map((f) => (
          <li key={f.key}>
            <button
              type="button"
              className="hx-scene3d__frame"
              data-status={f.status}
              onClick={() => f.status === FRAME_STATUS.ready && setShown(f)}
            >
              {f.status === FRAME_STATUS.ready ? (
                <img src={frameUrl(machine, id, f.key)} alt="" loading="lazy" />
              ) : (
                <span className="hx-scene3d__frameStatus">
                  {f.status === FRAME_STATUS.failed
                    ? t('next.scene3d.frameFailed')
                    : t('next.scene3d.frameBusy')}
                </span>
              )}
              <span className="hx-scene3d__framemeta">
                {f.cam?.n ?? t('next.scene3d.camFree')} ·{' '}
                {meta ? clock(timeOf(meta, f.frame)) : `#${f.frame}`}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );

  const checksTab = meta && (
    <div className="hx-scene3d__scroll">
      <p className="hx-scene3d__stats">
        {t('next.scene3d.stats', {
          objects: meta.stats.objects,
          tris: meta.stats.tris.toLocaleString(),
          out: meta.stats.trisOut.toLocaleString(),
        })}
      </p>
      {checks.length === 0 && <p className="hx-scene3d__empty">{t('next.scene3d.checksOk')}</p>}
      <ul className="hx-scene3d__checks">
        {checks.map((c) => (
          <li key={c.code}>
            <strong>{t(CHECK_KEYS[c.code], { n: c.count })}</strong>
            {c.names.length > 0 && <span className="hx-scene3d__dim"> {c.names.join(', ')}</span>}
          </li>
        ))}
      </ul>
    </div>
  );

  const tabs: { id: TTab; label: string; n?: number }[] = [
    { id: TAB.objects, label: t('next.scene3d.tabObjects') },
    { id: TAB.pins, label: t('next.scene3d.tabPins'), n: pins.filter((p) => !p.resolved).length },
    {
      id: TAB.frames,
      label: t('next.scene3d.tabFrames'),
      n: frames.length + (state?.clips.length ?? 0) + (state?.outputs.length ?? 0),
    },
    { id: TAB.checks, label: t('next.scene3d.tabChecks'), n: checks.length },
  ];

  // ---- the layout: the panel on the right folds away; a narrow page starts with it folded
  const root = useRef<HTMLDivElement | null>(null);
  const [side, setSide] = useState<boolean | null>(null);
  useLayoutEffect(() => {
    if (side === null) setSide((root.current?.clientWidth ?? 1200) >= NARROW_PX);
  }, [side]);
  const sideOpen = side ?? true;
  const openTab = (x: TTab) => {
    setTab(x);
    setSide(true);
  };

  // The pin's popover stands by its point and follows it while the view moves (read every frame while it is open).
  const stageRef = useRef<HTMLElement | null>(null);
  const [placed, setPlaced] = useState<IPlaced | null>(null);
  const draftPoint = draft?.p;
  useEffect(() => {
    if (!draftPoint) return;
    let raf = 0;
    const follow = () => {
      const at = viewer.current?.screenOf(draftPoint) ?? null;
      const el = stageRef.current;
      const next = at && el ? placePopover(at, el.clientWidth, el.clientHeight) : null;
      setPlaced((cur) => (samePlace(cur, next) ? cur : next));
      raf = requestAnimationFrame(follow);
    };
    raf = requestAnimationFrame(follow);
    return () => {
      cancelAnimationFrame(raf);
      setPlaced(null);
    };
  }, [draftPoint]);
  const anchor = draftPoint ? placed?.at : undefined;
  const pop = draftPoint ? placed : null;

  return (
    <div
      ref={root}
      className="hx-scene3d"
      data-embed={props.pane ? '1' : '0'}
      data-pins={props.canPin ? '1' : '0'}
      data-side={sideOpen ? 'open' : 'closed'}
    >
      <header className="hx-scene3d__bar">
        {props.pane && (
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.scene3d.back')}
            onClick={props.pane.onBack}
          >
            <Icon name="arrow-left" />
          </button>
        )}
        <h1 className="hx-scene3d__title" title={props.title}>
          {props.title}
        </h1>
        {state && (
          <span className="hx-scene3d__engine" title={t('next.scene3d.engineHint')}>
            {t('next.scene3d.engine', { engine: state.engine })}
          </span>
        )}
        <span className="hx-scene3d__grow" />
        {props.mode && <ModeButton mode={props.mode} />}
        {props.access && (
          <button
            type="button"
            className="hx-btn"
            data-variant="ghost"
            data-size="sm"
            onClick={props.access.onOpen}
          >
            <Icon name="share" size="sm" />
            <span className="hx-scene3d__wide">{props.access.label}</span>
          </button>
        )}
        <button
          type="button"
          className="hx-head-btn"
          aria-label={sideOpen ? t('next.scene3d.panelHide') : t('next.scene3d.panelShow')}
          title={sideOpen ? t('next.scene3d.panelHide') : t('next.scene3d.panelShow')}
          aria-pressed={sideOpen}
          aria-controls="hx-scene3d-side"
          onClick={() => setSide(!sideOpen)}
        >
          <Icon name="panel-right" />
        </button>
        {props.onToggleMax && (
          <button
            type="button"
            className="hx-head-btn"
            aria-label={t('next.scene3d.maximize')}
            aria-pressed={props.maxed}
            onClick={props.onToggleMax}
          >
            <Icon name={props.maxed ? 'minimize-2' : 'maximize-2'} />
          </button>
        )}
      </header>

      <div className="hx-scene3d__body">
        <div className="hx-scene3d__work">
          <section ref={stageRef} className="hx-scene3d__stage" aria-label={t('next.scene3d.stage')}>
            <div className="hx-scene3d__view">{stage()}</div>

            {loaded && meta && (
              <div className="hx-scene3d__tools" role="toolbar" aria-label={t('next.scene3d.tools')}>
                <label className="hx-scene3d__select" title={t('next.scene3d.camera')}>
                  <Icon name="camera" size="sm" />
                  <select
                    value={camera}
                    onChange={(e) => pickCamera(e.target.value)}
                    aria-label={t('next.scene3d.camera')}
                  >
                    <option value={FREE}>{t('next.scene3d.camFree')}</option>
                    {meta.cameras.map((c) => (
                      <option key={c.n} value={c.n}>
                        {c.n}
                      </option>
                    ))}
                  </select>
                </label>
                <button
                  type="button"
                  className="hx-scene3d__icon"
                  aria-label={t('next.scene3d.reset')}
                  title={t('next.scene3d.reset')}
                  onClick={() => (
                    viewer.current?.follow(''),
                    setFollowing(''),
                    viewer.current?.resetView(),
                    pickCamera(FREE)
                  )}
                >
                  <Icon name="locate" size="sm" />
                </button>
                <button
                  type="button"
                  className="hx-scene3d__icon hx-scene3d__follow"
                  aria-label={
                    following === ''
                      ? t('next.scene3d.follow')
                      : t('next.scene3d.followOn', { object: following })
                  }
                  aria-pressed={following !== ''}
                  disabled={camera !== FREE || (following === '' && selected === '')}
                  title={t('next.scene3d.followHint')}
                  onClick={() => {
                    const next = following === '' ? selected : '';
                    viewer.current?.follow(next);
                    setFollowing(next);
                  }}
                >
                  <Icon name="eye" size="sm" />
                  {following !== '' && <span className="hx-scene3d__wide">{following}</span>}
                </button>
                <span className="hx-scene3d__sep" aria-hidden="true" />
                <span className="hx-scene3d__seg" role="group" aria-label={t('next.scene3d.look')}>
                  {([VIEW_MODE.material, VIEW_MODE.clay, VIEW_MODE.wire] as const).map((m) => (
                    <button key={m} type="button" aria-pressed={mode === m} onClick={() => pickMode(m)}>
                      {t(MODE_KEYS[m])}
                    </button>
                  ))}
                </span>
                <span className="hx-scene3d__sep" aria-hidden="true" />
                <span className="hx-scene3d__select hx-scene3d__where" title={t('next.scene3d.where')}>
                  <BuilderPicker
                    value={builder}
                    onChange={setBuilder}
                    caps={['blender']}
                    base={pickerBase}
                    disabled={drafting}
                  />
                </span>
                <button
                  type="button"
                  className="hx-btn hx-scene3d__draftBtn"
                  data-variant="primary"
                  data-size="sm"
                  disabled={drafting}
                  onClick={draftHere}
                  title={t('next.scene3d.draftHint', { engine: state?.engine ?? '' })}
                >
                  <Icon name={drafting ? 'loader-circle' : 'image'} size="sm" />
                  <span className="hx-scene3d__wide">
                    {drafting ? t('next.scene3d.drafting') : t('next.scene3d.draft')}
                  </span>
                </button>
              </div>
            )}

            {failure && (
              <div className="hx-scene3d__toast" role="alert">
                {failure}
                <button type="button" aria-label={t('next.scene3d.dismiss')} onClick={() => setFailure(null)}>
                  <Icon name="x" size="sm" />
                </button>
              </div>
            )}

            {shown && (
              <figure className="hx-scene3d__draft">
                <img src={frameUrl(machine, id, shown.key)} alt={t('next.scene3d.renderAlt')} />
                <figcaption>
                  <span>
                    {t('next.scene3d.draftCaption', {
                      cam: shown.cam?.n ?? t('next.scene3d.camFree'),
                      time: meta ? clock(timeOf(meta, shown.frame)) : String(shown.frame),
                      engine: shown.engine ?? '',
                      where: shown.where ?? t('next.scene3d.here'),
                    })}
                  </span>
                  <button type="button" aria-label={t('next.scene3d.dismiss')} onClick={() => setShown(null)}>
                    <Icon name="x" size="sm" />
                  </button>
                </figcaption>
              </figure>
            )}

            {draft && props.canPin && pop && anchor && (
              <span
                className="hx-scene3d__dot"
                aria-hidden="true"
                style={{ left: anchor.x, top: anchor.y }}
              />
            )}
            {draft && props.canPin && (
              <form
                className="hx-scene3d__composer"
                data-anchored={pop ? '1' : '0'}
                data-side={pop?.side}
                style={pop ? { left: pop.left, top: pop.top } : undefined}
                onSubmit={(e) => {
                  e.preventDefault();
                  void savePin();
                }}
              >
                <p className="hx-scene3d__composerOn">
                  <Icon name="map-pin" size="sm" />
                  <span>
                    {draft.object
                      ? t('next.scene3d.pinOn', { object: draft.object })
                      : t('next.scene3d.pinOnScene')}
                    {meta && ` · ${clock(timeOf(meta, frame))}`}
                  </span>
                </p>
                <textarea
                  autoFocus
                  rows={2}
                  value={draft.text}
                  placeholder={t('next.scene3d.pinPlaceholder')}
                  onChange={(e) => setDraft({ ...draft, text: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                      e.preventDefault();
                      void savePin();
                    } else if (e.key === 'Escape') {
                      e.preventDefault();
                      setDraft(null);
                    }
                  }}
                />
                <div className="hx-scene3d__composerRow">
                  <button
                    type="button"
                    className="hx-btn"
                    data-variant="ghost"
                    data-size="sm"
                    onClick={() => setDraft(null)}
                  >
                    {t('next.scene3d.cancel')}
                  </button>
                  <button
                    type="submit"
                    className="hx-btn"
                    data-variant="primary"
                    data-size="sm"
                    disabled={busy || !draft.text.trim()}
                  >
                    {t('next.scene3d.pinSave')}
                  </button>
                </div>
              </form>
            )}
            {!draft && loaded && props.canPin && (
              <p className="hx-scene3d__hint">{t('next.scene3d.pinHint')}</p>
            )}
          </section>

          {loaded && meta && (
            <Timeline
              meta={meta}
              frame={frame}
              playing={playing}
              pins={pins}
              activePin={activePin}
              frames={frames}
              onSeek={seek}
              onTogglePlay={togglePlay}
              onPin={(pin) => {
                goToPin(pin);
                openTab(TAB.pins);
              }}
              onFrame={(f) => {
                if (f.status === FRAME_STATUS.ready) setShown(f);
                openTab(TAB.frames);
              }}
            />
          )}
        </div>

        <aside id="hx-scene3d-side" className="hx-scene3d__side" hidden={!sideOpen}>
          <div className="hx-scene3d__tabs" role="tablist">
            {tabs.map((x) => (
              <button
                key={x.id}
                type="button"
                role="tab"
                aria-selected={tab === x.id}
                onClick={() => setTab(x.id)}
              >
                {x.label}
                {x.n ? <span className="hx-scene3d__badge">{x.n}</span> : null}
              </button>
            ))}
            <button
              type="button"
              className="hx-scene3d__fold-side"
              aria-label={t('next.scene3d.panelHide')}
              onClick={() => setSide(false)}
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
          {tab === TAB.objects && (objectTab ?? <p className="hx-scene3d__empty">{waiting ?? ''}</p>)}
          {tab === TAB.pins && pinsTab}
          {tab === TAB.frames && framesTab}
          {tab === TAB.checks && (checksTab ?? <p className="hx-scene3d__empty">{waiting ?? ''}</p>)}
        </aside>
      </div>
    </div>
  );
};

/** Narrower than this the panel starts folded and opens over the stage (scene3d.css has the same width). */
const NARROW_PX = 760;
const POPOVER_W = 280;
const POPOVER_H = 150;

interface IPlaced {
  at: { x: number; y: number };
  left: number;
  top: number;
  side: 'left' | 'right';
}

/** The popover beside its point: to the right when it fits, else to the left; inside the stage. */
const placePopover = (at: { x: number; y: number }, W: number, H: number): IPlaced => {
  const w = Math.min(POPOVER_W, W - 24);
  const right = at.x + 16 + w <= W - 12;
  const left = right ? at.x + 16 : Math.max(12, at.x - 16 - w);
  const top = Math.min(Math.max(12, at.y - 28), Math.max(12, H - POPOVER_H - 12));
  return { at, left, top, side: right ? 'right' : 'left' };
};

const samePlace = (a: IPlaced | null, b: IPlaced | null): boolean =>
  a === b ||
  (!!a && !!b && Math.abs(a.at.x - b.at.x) < 0.5 && Math.abs(a.at.y - b.at.y) < 0.5 && a.top === b.top);

const CHECK_KEYS = {
  [CHECK.tris]: 'next.scene3d.checkTris',
  [CHECK.textures]: 'next.scene3d.checkTextures',
  [CHECK.scale]: 'next.scene3d.checkScale',
  [CHECK.offCamera]: 'next.scene3d.checkOffCamera',
  [CHECK.emptyAtSave]: 'next.scene3d.checkEmptyAtSave',
} as const;

const MODE_KEYS = {
  [VIEW_MODE.material]: 'next.scene3d.modeMaterial',
  [VIEW_MODE.clay]: 'next.scene3d.modeClay',
  [VIEW_MODE.wire]: 'next.scene3d.modeWire',
} as const;

/** The same view, to the precision a frame is cached by (the runtime rounds to a thousandth). */
const sameView = (a: IScene3dCam, b: IScene3dCam): boolean => {
  if (a.n || b.n) return a.n === b.n;
  const near = (x: readonly number[], y: readonly number[]) =>
    x.every((v, i) => Math.abs(v - (y[i] ?? 0)) < 0.0006);
  return near(a.pos, b.pos) && near(a.target, b.target) && Math.abs(a.fov - b.fov) < 0.0006;
};

export default Scene3dView;

/** The page's base of the builders' route: BuilderPicker adds /api/platform/builders itself, the runtime's own prefix may differ. */
const builderBase = (full: string): string => {
  const tail = '/api/platform/builders';
  return full.endsWith(tail) ? full.slice(0, -tail.length) : '';
};
