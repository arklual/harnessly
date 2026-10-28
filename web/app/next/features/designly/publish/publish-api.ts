// «Опубликовать»: the runtime of the machine copies the chosen pages to the hub as a version of a site and answers with
// the site's address. The hub keeps the files, so the site opens with the machine off.
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export type TSiteAccess = 'team' | 'link';

export interface IPublishRequest {
  project: string;
  name: string;
  access: TSiteAccess;
  /** The chosen pages' paths; null is all of them. */
  pages: readonly string[] | null;
}

export interface IPublished {
  url: string;
  version: number;
  name: string;
}

/** The site's name for a design: Latin letters, digits and hyphens, as the hub takes it; empty when the title has none. */
export const siteNameOf = (title: string): string =>
  title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 30)
    .replace(/-+$/, '');

/** A name the hub accepts: 1–30 of a–z, 0–9 and a hyphen, not at the ends and never two in a row. */
export const validSiteName = (name: string): boolean =>
  /^[a-z0-9]([a-z0-9-]{0,28}[a-z0-9])?$/.test(name) && !name.includes('--');

export const publishDesign = (machine: MachineId, req: IPublishRequest): Promise<IPublished> =>
  runtimeJson<IPublished>(
    machine,
    platformPath('/design/publish'),
    postJson({ project: req.project, name: req.name, access: req.access, pages: req.pages }),
  );
