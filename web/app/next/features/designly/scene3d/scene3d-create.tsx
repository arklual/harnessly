// «3D scene» of the Create menu: the person picks a blender-lab project of the machine (the ones it finds beside the
// library, or a folder typed in), the .blend that is the scene and the script that builds it, and the document opens. Nothing
// is rendered or sent anywhere here: the document is a pointer at the project's files.
import { useEffect, useRef, useState } from 'react';
import { t } from '@/next/i18n';
import { Modal } from '@/next/features/designly/sdk/ui';
import { Button } from '@/next/ui/button';
import type { MachineId } from '@/next/protocol/v2';
import { createScene3d, discoverScene3d, type IScene3dProject } from '@/next/features/designly/scene3d/api';

export interface IScene3dCreateProps {
  machine: MachineId;
  open: boolean;
  onClose: () => void;
  /** The scene is made: open it. */
  onCreated: (id: string) => void;
}

const NONE = '';
const base = (file: string): string => file.replace(/^.*\//, '').replace(/\.[^.]+$/, '');

/** The script that builds a .blend: the one named like it (retro_convertible.blend ← scenes/retro_convertible.py), else the first. */
export const scriptFor = (blend: string, scripts: readonly string[]): string => {
  const name = base(blend).replace(/_(clay|color|pack)$/, '');
  return scripts.find((s) => base(s) === name) ?? scripts.find((s) => base(s) === base(blend)) ?? NONE;
};

const Scene3dCreate = ({ machine, open, onClose, onCreated }: IScene3dCreateProps) => {
  const [projects, setProjects] = useState<IScene3dProject[] | null>(null);
  const [root, setRoot] = useState('');
  const [blend, setBlend] = useState('');
  const [script, setScript] = useState(NONE);
  const [title, setTitle] = useState('');
  const [typed, setTyped] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const typedRef = useRef<HTMLInputElement>(null);

  const pick = (list: IScene3dProject[], at: string) => {
    const p = list.find((x) => x.root === at) ?? list[0];
    if (!p) {
      setRoot('');
      setBlend('');
      setScript(NONE);
      return;
    }
    const b = p.blends[0] ?? '';
    setRoot(p.root);
    setBlend(b);
    setScript(scriptFor(b, p.scripts));
    setTitle(b ? base(b).replace(/_/g, ' ') : p.name);
  };

  useEffect(() => {
    if (!open) return;
    const abort = new AbortController();
    discoverScene3d(machine, '', abort.signal).then(
      (list) => {
        setError(null);
        setProjects(list);
        pick(list, '');
      },
      (e: Error) => {
        if (abort.signal.aborted) return;
        setProjects([]);
        setError(e.message);
      },
    );
    return () => abort.abort();
  }, [machine, open]);

  const look = async () => {
    const dir = typed.trim();
    if (!dir) return;
    setError(null);
    try {
      const found = await discoverScene3d(machine, dir);
      setProjects((cur) => [...found, ...(cur ?? []).filter((p) => !found.some((f) => f.root === p.root))]);
      pick(found, found[0]?.root ?? '');
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  };

  const current = projects?.find((p) => p.root === root) ?? null;
  const make = async () => {
    if (!current || !blend || busy) return;
    setBusy(true);
    setError(null);
    try {
      const made = await createScene3d(machine, { title: title.trim(), root: current.root, blend, script });
      onCreated(made.id);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('next.scene3d.newTitle')}
      lead={t('next.scene3d.newLead')}
      boxClass="hx-scene3d-new"
      initialFocus={typedRef}
    >
      <div className="hx-scene3d-new__body">
        {projects === null && <p className="hx-scene3d__empty">{t('next.scene3d.newLooking')}</p>}
        {projects !== null && projects.length === 0 && (
          <p className="hx-scene3d__empty">{t('next.scene3d.newNone')}</p>
        )}
        {projects !== null && projects.length > 0 && (
          <>
            <label className="hx-scene3d-new__field">
              <span>{t('next.scene3d.newProject')}</span>
              <select value={root} onChange={(e) => pick(projects, e.target.value)}>
                {projects.map((p) => (
                  <option key={p.root} value={p.root}>
                    {p.name} · {p.root}
                  </option>
                ))}
              </select>
            </label>
            <label className="hx-scene3d-new__field">
              <span>{t('next.scene3d.newBlend')}</span>
              <select
                value={blend}
                onChange={(e) => {
                  setBlend(e.target.value);
                  setScript(scriptFor(e.target.value, current?.scripts ?? []));
                  setTitle(base(e.target.value).replace(/_/g, ' '));
                }}
              >
                {(current?.blends ?? []).map((b) => (
                  <option key={b} value={b}>
                    {b}
                  </option>
                ))}
              </select>
            </label>
            <label className="hx-scene3d-new__field">
              <span>{t('next.scene3d.newScript')}</span>
              <select value={script} onChange={(e) => setScript(e.target.value)}>
                <option value={NONE}>{t('next.scene3d.newNoScript')}</option>
                {(current?.scripts ?? []).map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </label>
            <label className="hx-scene3d-new__field">
              <span>{t('next.scene3d.newName')}</span>
              <input value={title} onChange={(e) => setTitle(e.target.value)} />
            </label>
          </>
        )}
        <div className="hx-scene3d-new__field">
          <span>{t('next.scene3d.newFolder')}</span>
          <div className="hx-scene3d-new__row">
            <input
              ref={typedRef}
              value={typed}
              placeholder="/home/me/blender-lab"
              onChange={(e) => setTyped(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') void look();
              }}
            />
            <Button variant="ghost" onClick={() => void look()}>
              {t('next.scene3d.newLook')}
            </Button>
          </div>
        </div>
        {error && (
          <p className="hx-scene3d__error-line" role="alert">
            {error}
          </p>
        )}
      </div>
      <div className="hx-modal__foot">
        <Button variant="ghost" onClick={onClose}>
          {t('next.scene3d.cancel')}
        </Button>
        <Button variant="primary" busy={busy} disabled={!current || !blend} onClick={() => void make()}>
          {t('next.scene3d.newCreate')}
        </Button>
      </div>
    </Modal>
  );
};

export default Scene3dCreate;
