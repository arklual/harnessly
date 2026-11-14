// The crumbs of a place in the pane: from the chat's folder for a path in it, from «Машина» for an absolute one.
import { t } from '@/next/i18n';
import type { ICrumb } from '@/next/features/pane/pane-head';

/** The list of the workspace's places: the top of «Машина». */
export const MACHINE = '/';

export const nameOf = (path: string) => path.split('/').filter(Boolean).pop() ?? path;
export const trimmed = (dir: string) => dir.replace(/\/+$/, '') || MACHINE;

/** The steps under a base: `a/b` of `base` is base/a, base/a/b. */
const stepsOf = (rel: string, join: (sub: string) => string) => {
  const parts = rel.split('/').filter(Boolean);
  return parts.map((name, i) => ({ name, path: join(parts.slice(0, i + 1).join('/')) }));
};

/** A folder outside the places, said short: its name, and for a design's `<id>/project` the design's folder; the whole
 *  path is the hover's. */
const shortFolder = (dir: string): ICrumb => {
  const parts = dir.split('/').filter(Boolean);
  const last = parts.at(-1) ?? dir;
  const name = last === 'project' ? (parts.at(-2) ?? last) : last;
  return { name, path: dir, title: dir };
};

/** A path of the machine: «Машина», the place it lies in, the folders under that place. A file opened by itself shows
 *  its folder and its name, a folder still loading its whole path in one step. */
const machineCrumbs = (path: string, top: string | undefined, file: boolean): ICrumb[] => {
  const head = { name: t('next.files.machine'), path: MACHINE };
  if (path === MACHINE) return [head];
  if (top && (path === top || path.startsWith(`${top}/`)))
    return [head, { name: top, path: top }, ...stepsOf(path.slice(top.length), (sub) => `${top}/${sub}`)];
  if (file) {
    const dir = path.slice(0, path.lastIndexOf('/')) || MACHINE;
    return [head, shortFolder(dir), { name: nameOf(path), path, title: path }];
  }
  return [head, shortFolder(path)];
};

/** The crumbs of a place: from the chat's folder (`root`) for a path in it, from the machine for an absolute one; `top`:
 *  the place of the workspace the listing names, `file`: the path is a file's. */
export const crumbsOf = (root: string, path: string, top: string | undefined, file: boolean): ICrumb[] => {
  const home = trimmed(root);
  if (path.startsWith('/')) return machineCrumbs(path, top, file);
  return [{ name: nameOf(home), path: '' }, ...stepsOf(path, (sub) => sub)];
};
