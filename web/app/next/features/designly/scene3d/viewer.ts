// The 3D scene's viewer: three.js on a canvas, no React (scene3d-view.tsx drives it). It loads the exported .glb, orbits
// around it, shows the scene's cameras as the render has them (their sampled paths, scene-model.ts poseAt), picks an object
// or a point on a surface with a click, plays the animation, draws the pins and takes a picture of the viewport. Loaded
// lazily: three.js is a chunk of its own.
import {
  AmbientLight,
  Box3,
  BufferGeometry,
  CanvasTexture,
  Color,
  DirectionalLight,
  GridHelper,
  Group,
  HemisphereLight,
  Material,
  Mesh,
  MeshBasicMaterial,
  MeshStandardMaterial,
  Object3D,
  PerspectiveCamera,
  Raycaster,
  Scene,
  Sprite,
  SpriteMaterial,
  Vector2,
  Vector3,
  WebGLRenderer,
  AnimationMixer,
  Box3Helper,
  type AnimationClip,
  type Intersection,
} from 'three';
import { GLTFLoader } from 'three/examples/jsm/loaders/GLTFLoader.js';
import { OrbitControls } from 'three/examples/jsm/controls/OrbitControls.js';
import {
  blenderToThree,
  canvasFov,
  poseAt,
  roundVec,
  threeToBlender,
  timeOf,
} from '@/next/features/designly/scene3d/scene-model';
import {
  VIEW_MODE,
  type IScene3dCam,
  type IScene3dMeta,
  type IScene3dPin,
  type TVec3,
  type TViewMode,
} from '@/next/features/designly/scene3d/types';

export interface IViewerEvents {
  /** A click on the model: the object (its Blender name) and the point on its surface (Blender's axes); null: on nothing. */
  onPick: (object: string | null, point: TVec3 | null) => void;
  onPin: (id: string) => void;
  /** The playing animation reached a frame. */
  onFrame: (frame: number) => void;
  onPlaying: (playing: boolean) => void;
}

const FREE_FOV = 40;
const PIN_BASE = 0.034;
const CLICK_PX = 5;

const isMesh = (o: Object3D): o is Mesh => (o as Mesh).isMesh === true;

/** The Blender name of a three.js node: GLTFLoader cleans names (dots, spaces) and keeps the original in userData. */
const blenderName = (o: Object3D): string => {
  const own = (o.userData as { name?: unknown }).name;
  return typeof own === 'string' ? own : o.name;
};

export class SceneViewer {
  private readonly renderer: WebGLRenderer;
  private readonly scene = new Scene();
  private readonly orbitCam = new PerspectiveCamera(FREE_FOV, 1, 0.05, 6000);
  private readonly controls: OrbitControls;
  private readonly host: HTMLElement;
  private readonly meta: IScene3dMeta;
  private readonly ev: IViewerEvents;
  private readonly model = new Group();
  private readonly pinGroup = new Group();
  private readonly nodes = new Map<string, Object3D>();
  private readonly originals = new Map<Mesh, Material | Material[]>();
  private readonly raycaster = new Raycaster();
  private readonly clay = new MeshStandardMaterial({ color: 0xc9c9cc, roughness: 0.85, metalness: 0 });
  private readonly wire = new MeshBasicMaterial({ color: 0x20242b, wireframe: true });
  private readonly selection = new Box3Helper(new Box3(), new Color(0xffb020));
  private mixer: AnimationMixer | null = null;
  private clipSeconds = 0;
  private mode: TViewMode = VIEW_MODE.material;
  private camName = '';
  private frame: number;
  private playing = false;
  private raf = 0;
  private last = 0;
  private emitted = 0;
  private down: { x: number; y: number } | null = null;
  private selected = '';
  private disposed = false;
  private readonly observer: ResizeObserver;
  private home: IScene3dCam | null = null;

  constructor(host: HTMLElement, meta: IScene3dMeta, events: IViewerEvents) {
    this.host = host;
    this.meta = meta;
    this.ev = events;
    this.frame = meta.start;
    this.renderer = new WebGLRenderer({ antialias: true, alpha: false });
    this.renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2));
    this.renderer.domElement.className = 'hx-scene3d__canvas';
    host.appendChild(this.renderer.domElement);
    this.scene.background = new Color(0xe7e8ec);
    this.scene.add(new HemisphereLight(0xffffff, 0x8a8f99, 1.4));
    const sun = new DirectionalLight(0xffffff, 2.2);
    sun.position.set(6, 12, 8);
    this.scene.add(new AmbientLight(0xffffff, 0.2), sun, this.model, this.pinGroup);
    this.selection.visible = false;
    this.scene.add(this.selection);
    this.orbitCam.position.set(6, 4, 8);
    this.controls = new OrbitControls(this.orbitCam, this.renderer.domElement);
    this.controls.enableDamping = true;
    this.controls.dampingFactor = 0.12;
    this.controls.addEventListener('change', () => this.draw());
    const el = this.renderer.domElement;
    el.addEventListener('pointerdown', this.onDown);
    el.addEventListener('pointerup', this.onUp);
    this.observer = new ResizeObserver(() => this.resize());
    this.observer.observe(host);
    this.resize();
  }

  // ---- loading

  async load(url: string, signal?: AbortSignal): Promise<void> {
    const gltf = await new GLTFLoader().loadAsync(url);
    if (this.disposed || signal?.aborted) return;
    this.model.add(gltf.scene);
    gltf.scene.traverse((o) => {
      this.nodes.set(blenderName(o), o);
      if (isMesh(o)) this.originals.set(o, o.material);
      // The scene's own cameras are drawn from their sampled paths (the glTF's camera nodes know no constraints).
      if ((o as PerspectiveCamera).isCamera) o.visible = false;
    });
    // What the render hides (cutters, helpers) is not shown: a node of such an object is off.
    for (const o of this.meta.objects)
      if (o.hidden) this.nodes.get(o.n)?.traverse((n) => (n.visible = false));
    this.setMode(this.mode);
    this.mixer = new AnimationMixer(gltf.scene);
    this.clipSeconds = 0;
    for (const clip of gltf.animations as AnimationClip[]) {
      this.mixer.clipAction(clip).play();
      this.clipSeconds = Math.max(this.clipSeconds, clip.duration);
    }
    this.seek(this.meta.start);
    this.frameModel();
    this.home = this.getView();
    this.draw();
  }

  /** Puts the orbit on the model: its bounds without the very big things (a ground that goes to the horizon). */
  private frameModel(): void {
    const boxes: Box3[] = [];
    this.model.updateMatrixWorld(true);
    this.model.traverse((o) => {
      if (isMesh(o)) boxes.push(new Box3().setFromObject(o));
    });
    const sizes = boxes.map((b) => b.getSize(new Vector3()).length()).sort((a, b) => a - b);
    const median = sizes[Math.floor(sizes.length / 2)] ?? 1;
    const keep = new Box3();
    for (const b of boxes) if (b.getSize(new Vector3()).length() <= Math.max(median * 25, 1)) keep.union(b);
    if (keep.isEmpty()) keep.set(new Vector3(-3, 0, -3), new Vector3(3, 2, 3));
    let center = keep.getCenter(new Vector3());
    const radius = Math.max(keep.getSize(new Vector3()).length() / 2, 0.5);
    let dist = radius * 2.1;
    // A scene with cameras is about what they look at: start there, a few metres off, not at the whole of a long road.
    const first = this.meta.cameras.find((c) => c.n === (this.meta.camera || '')) ?? this.meta.cameras[0];
    if (first) {
      const p = poseAt(first, this.meta.start);
      const look = new Vector3().fromArray(blenderToThree(p.target));
      const eye = new Vector3().fromArray(blenderToThree(p.pos));
      center = look;
      dist = Math.min(Math.max(eye.distanceTo(look) * 1.6, 5), dist);
    }
    this.controls.target.copy(center);
    this.orbitCam.position.copy(center).add(new Vector3(0.9, 0.55, 1.1).normalize().multiplyScalar(dist));
    this.orbitCam.far = Math.max(2000, radius * 400);
    this.orbitCam.near = Math.max(0.02, radius / 500);
    this.orbitCam.updateProjectionMatrix();
    const grid = new GridHelper(Math.ceil(radius * 6), Math.ceil(radius * 6), 0x9aa0ab, 0xc3c7d0);
    grid.position.set(center.x, keep.min.y, center.z);
    (grid.material as Material).opacity = 0.5;
    (grid.material as Material).transparent = true;
    this.scene.add(grid);
    this.controls.update();
  }

  // ---- looks

  setMode(mode: TViewMode): void {
    this.mode = mode;
    for (const [mesh, original] of this.originals) {
      mesh.material = mode === VIEW_MODE.clay ? this.clay : mode === VIEW_MODE.wire ? this.wire : original;
    }
    this.scene.background = new Color(mode === VIEW_MODE.wire ? 0xf4f5f7 : 0xe7e8ec);
    this.draw();
  }

  // ---- the camera

  /** The camera on screen: a camera of the scene by name, or '' for the free orbit. */
  camera(): string {
    return this.camName;
  }

  /** Shows the scene's camera `name` as the render has it at the current frame; '' goes back to the free orbit. */
  useCamera(name: string): void {
    if (name === this.camName) return;
    if (this.camName === '' && name !== '') this.freeView = this.getView();
    this.camName = name;
    this.controls.enabled = name === '';
    if (name === '' && this.freeView) this.setOrbit(this.freeView);
    this.draw();
  }

  private freeView: IScene3dCam | null = null;

  private setOrbit(v: IScene3dCam): void {
    this.orbitCam.position.fromArray(blenderToThree(v.pos));
    this.controls.target.fromArray(blenderToThree(v.target));
    this.orbitCam.userData.fov = v.fov > 0 ? v.fov : FREE_FOV;
    this.controls.update();
  }

  /** Goes to a saved view: its camera of the scene when it names one that is there, else the free orbit at the place. */
  goTo(v: IScene3dCam, frame?: number): void {
    if (frame !== undefined) this.seek(frame);
    if (v.n && this.meta.cameras.some((c) => c.n === v.n)) {
      this.useCamera(v.n);
      return;
    }
    this.camName = '';
    this.controls.enabled = true;
    this.setOrbit(v);
    this.draw();
  }

  /** Where the viewport is now, in Blender's axes: what a pin keeps and a draft frame is drawn from. */
  getView(): IScene3dCam {
    if (this.camName) {
      const cam = this.meta.cameras.find((c) => c.n === this.camName);
      if (cam) {
        const p = poseAt(cam, this.frame);
        return {
          n: cam.n,
          pos: roundVec(p.pos),
          target: roundVec(p.target),
          fov: Math.round(p.fov * 100) / 100,
        };
      }
    }
    const fov = (this.orbitCam.userData.fov as number | undefined) ?? FREE_FOV;
    return {
      pos: roundVec(threeToBlender(this.orbitCam.position.toArray() as unknown as TVec3)),
      target: roundVec(threeToBlender(this.controls.target.toArray() as unknown as TVec3)),
      fov,
    };
  }

  resetView(): void {
    if (this.home) this.goTo({ ...this.home, n: undefined });
  }

  /** The render's aspect fitted in the canvas: the guide the page draws over it, in px of the canvas. */
  guide(): { x: number; y: number; w: number; h: number } {
    const W = this.host.clientWidth || 1;
    const H = this.host.clientHeight || 1;
    const ra = this.meta.res[0] / Math.max(this.meta.res[1], 1);
    const w = W / H >= ra ? H * ra : W;
    const h = W / H >= ra ? H : W / ra;
    return { x: (W - w) / 2, y: (H - h) / 2, w, h };
  }

  private active(): PerspectiveCamera {
    const aspect = (this.host.clientWidth || 1) / (this.host.clientHeight || 1);
    const ra = this.meta.res[0] / Math.max(this.meta.res[1], 1);
    if (this.camName) {
      const cam = this.meta.cameras.find((c) => c.n === this.camName);
      if (cam) {
        const p = poseAt(cam, this.frame);
        this.orbitScene.position.fromArray(blenderToThree(p.pos));
        this.orbitScene.up.set(0, 1, 0);
        this.orbitScene.lookAt(new Vector3().fromArray(blenderToThree(p.target)));
        this.orbitScene.fov = canvasFov(p.fov, ra, aspect);
        this.orbitScene.near = Math.max(cam.near, 0.02);
        this.orbitScene.far = Math.max(cam.far, 100);
        this.orbitScene.aspect = aspect;
        this.orbitScene.updateProjectionMatrix();
        this.orbitScene.updateMatrixWorld();
        return this.orbitScene;
      }
    }
    this.orbitCam.fov = canvasFov((this.orbitCam.userData.fov as number | undefined) ?? FREE_FOV, ra, aspect);
    this.orbitCam.aspect = aspect;
    this.orbitCam.updateProjectionMatrix();
    return this.orbitCam;
  }

  private readonly orbitScene = new PerspectiveCamera(40, 1, 0.1, 1000);

  // ---- time

  /** Puts the model at a frame of the animation. */
  seek(frame: number): void {
    this.frame = Math.min(this.meta.end, Math.max(this.meta.start, Math.round(frame)));
    this.mixer?.setTime(Math.min(timeOf(this.meta, this.frame), this.clipSeconds || Infinity));
    this.refreshSelection();
    this.draw();
  }

  currentFrame(): number {
    return this.frame;
  }

  play(): void {
    if (this.playing) return;
    if (this.frame >= this.meta.end) this.seek(this.meta.start);
    this.playing = true;
    this.last = performance.now();
    this.ev.onPlaying(true);
    const tick = (now: number) => {
      if (!this.playing || this.disposed) return;
      const next = this.frame + ((now - this.last) / 1000) * (this.meta.fps || 24);
      this.last = now;
      if (next >= this.meta.end) {
        this.seek(this.meta.end);
        this.pause();
        this.ev.onFrame(this.frame);
        return;
      }
      this.seekFloat(next);
      if (now - this.emitted > 66) {
        this.emitted = now;
        this.ev.onFrame(this.frame);
      }
      this.raf = requestAnimationFrame(tick);
    };
    this.raf = requestAnimationFrame(tick);
  }

  /** Between frames while playing: the model at the exact time, the frame number rounded down. */
  private seekFloat(frame: number): void {
    this.frame = Math.floor(frame);
    this.mixer?.setTime(Math.min(timeOf(this.meta, frame), this.clipSeconds || Infinity));
    this.refreshSelection();
    this.draw();
  }

  pause(): void {
    if (!this.playing) return;
    this.playing = false;
    cancelAnimationFrame(this.raf);
    this.ev.onPlaying(false);
  }

  isPlaying(): boolean {
    return this.playing;
  }

  // ---- picking

  private readonly onDown = (e: PointerEvent) => {
    this.down = { x: e.clientX, y: e.clientY };
  };

  private readonly onUp = (e: PointerEvent) => {
    const d = this.down;
    this.down = null;
    if (!d || Math.hypot(e.clientX - d.x, e.clientY - d.y) > CLICK_PX) return;
    const rect = this.renderer.domElement.getBoundingClientRect();
    const ndc = new Vector2(
      ((e.clientX - rect.left) / rect.width) * 2 - 1,
      -(((e.clientY - rect.top) / rect.height) * 2 - 1),
    );
    this.raycaster.setFromCamera(ndc, this.active());
    const pin = this.raycaster.intersectObjects(this.pinGroup.children, false)[0];
    if (pin) {
      this.ev.onPin(String(pin.object.userData.pin));
      return;
    }
    const hit = this.raycaster
      .intersectObject(this.model, true)
      .find((h: Intersection) => isMesh(h.object) && h.object.visible);
    if (!hit) {
      this.ev.onPick(null, null);
      return;
    }
    const known = new Set(this.meta.objects.map((o) => o.n));
    let name: string | null = null;
    for (let o: Object3D | null = hit.object; o; o = o.parent) {
      const n = blenderName(o);
      if (known.has(n)) {
        name = n;
        break;
      }
    }
    this.ev.onPick(name, roundVec(threeToBlender(hit.point.toArray() as unknown as TVec3)));
  };

  /** Marks an object (its Blender name) with a box; '' clears it. */
  select(name: string): void {
    this.selected = name;
    this.refreshSelection();
    this.draw();
  }

  private refreshSelection(): void {
    const node = this.selected ? this.nodes.get(this.selected) : undefined;
    if (!node) {
      this.selection.visible = false;
      return;
    }
    node.updateWorldMatrix(true, true);
    const box = new Box3().setFromObject(node);
    this.selection.visible = !box.isEmpty();
    if (!box.isEmpty()) this.selection.box.copy(box);
  }

  // ---- pins

  setPins(pins: readonly IScene3dPin[], active: string): void {
    for (const c of [...this.pinGroup.children]) {
      this.pinGroup.remove(c);
      const s = c as Sprite;
      (s.material as SpriteMaterial).map?.dispose();
      s.material.dispose();
    }
    pins.forEach((pin, i) => {
      const p = pin.scene.p;
      if (!p) return;
      const sprite = new Sprite(
        new SpriteMaterial({
          map: pinTexture(i + 1, pin.resolved, pin.id === active),
          depthTest: false,
          transparent: true,
        }),
      );
      sprite.material.sizeAttenuation = false;
      sprite.scale.setScalar(pin.id === active ? PIN_BASE * 1.35 : PIN_BASE);
      sprite.position.fromArray(blenderToThree(p));
      sprite.renderOrder = 999;
      sprite.userData.pin = pin.id;
      this.pinGroup.add(sprite);
    });
    this.draw();
  }

  // ---- picture

  /** The viewport as a JPEG data URL, at most `max` px wide. */
  snapshot(max = 480, quality = 0.72): string {
    this.render();
    const src = this.renderer.domElement;
    const k = Math.min(1, max / Math.max(src.width, 1));
    const out = document.createElement('canvas');
    out.width = Math.max(1, Math.round(src.width * k));
    out.height = Math.max(1, Math.round(src.height * k));
    out.getContext('2d')?.drawImage(src, 0, 0, out.width, out.height);
    return out.toDataURL('image/jpeg', quality);
  }

  // ---- drawing

  private pending = false;

  private draw(): void {
    if (this.pending || this.disposed) return;
    this.pending = true;
    requestAnimationFrame(() => {
      this.pending = false;
      if (!this.disposed) this.render();
    });
  }

  private render(): void {
    if (this.camName === '') this.controls.update();
    this.renderer.render(this.scene, this.active());
  }

  private resize(): void {
    const w = Math.max(1, this.host.clientWidth);
    const h = Math.max(1, this.host.clientHeight);
    this.renderer.setSize(w, h, false);
    this.draw();
  }

  dispose(): void {
    this.disposed = true;
    this.pause();
    this.observer.disconnect();
    this.controls.dispose();
    const el = this.renderer.domElement;
    el.removeEventListener('pointerdown', this.onDown);
    el.removeEventListener('pointerup', this.onUp);
    this.scene.traverse((o) => {
      if (isMesh(o)) (o.geometry as BufferGeometry).dispose();
    });
    for (const original of this.originals.values()) {
      for (const m of Array.isArray(original) ? original : [original]) m.dispose();
    }
    this.clay.dispose();
    this.wire.dispose();
    this.renderer.dispose();
    el.remove();
  }
}

const textures = new Map<string, CanvasTexture>();

/** A round numbered marker: open pins are warm, resolved ones grey, the active one ringed. */
function pinTexture(n: number, resolved: boolean, active: boolean): CanvasTexture {
  const key = `${n}:${resolved}:${active}`;
  const have = textures.get(key);
  if (have) return have;
  const c = document.createElement('canvas');
  c.width = c.height = 96;
  const g = c.getContext('2d');
  if (g) {
    g.beginPath();
    g.arc(48, 48, 40, 0, Math.PI * 2);
    g.fillStyle = resolved ? '#8b909a' : '#e8590c';
    g.fill();
    g.lineWidth = active ? 9 : 5;
    g.strokeStyle = '#ffffff';
    g.stroke();
    g.fillStyle = '#ffffff';
    g.font = '700 44px system-ui, sans-serif';
    g.textAlign = 'center';
    g.textBaseline = 'middle';
    g.fillText(String(n), 48, 51);
  }
  const tex = new CanvasTexture(c);
  textures.set(key, tex);
  return tex;
}
