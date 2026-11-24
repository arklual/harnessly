import {
  AmbientLight,
  Box3,
  DirectionalLight,
  LoadingManager,
  PerspectiveCamera,
  Scene,
  Vector3,
  WebGLRenderer,
} from 'three';
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

export async function showModel(body: Blob, canvas: HTMLCanvasElement, signal: AbortSignal) {
  const manager = new LoadingManager();
  manager.setURLModifier((url) => {
    if (/^(blob:|data:)/.test(url)) return url;
    throw new Error('Model references an external asset');
  });
  const model = await new GLTFLoader(manager).parseAsync(await body.arrayBuffer(), '');
  if (signal.aborted) return;
  const renderer = new WebGLRenderer({ canvas, antialias: true });
  const scene = new Scene();
  scene.add(model.scene, new AmbientLight(0xffffff, 2));
  const light = new DirectionalLight(0xffffff, 3);
  light.position.set(2, 3, 4);
  scene.add(light);
  const bounds = new Box3().setFromObject(model.scene);
  const radius = Math.max(bounds.getSize(new Vector3()).length() / 2, 0.001);
  const center = bounds.getCenter(new Vector3());
  const camera = new PerspectiveCamera(45, 1, radius / 100, radius * 100);
  const controls = new OrbitControls(camera, canvas);
  controls.target.copy(center);
  camera.position.copy(center).add(new Vector3(1, 0.6, 1).multiplyScalar(2 * radius));
  controls.update();
  function draw() {
    const { clientWidth: w, clientHeight: h } = canvas;
    renderer.setSize(w, h, false);
    camera.aspect = w / Math.max(1, h);
    camera.updateProjectionMatrix();
    renderer.render(scene, camera);
  }
  const observer = new ResizeObserver(draw);
  observer.observe(canvas);
  controls.addEventListener('change', draw);
  draw();
  signal.addEventListener(
    'abort',
    () => {
      observer.disconnect();
      controls.dispose();
      renderer.dispose();
      renderer.forceContextLoss();
    },
    { once: true },
  );
}
