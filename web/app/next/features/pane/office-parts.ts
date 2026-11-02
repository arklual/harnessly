// What Word and PowerPoint files share: a zip of XML parts that point at each other, pictures among them. An edit changes a part or
// two and the rest goes back as it came.
import { strFromU8, strToU8, zipSync, type Unzipped, type Zippable } from 'fflate';

export const parseXml = (text: string): Document => {
  const doc = new DOMParser().parseFromString(text, 'application/xml');
  if (doc.getElementsByTagName('parsererror').length > 0) throw new Error('bad xml');
  return doc;
};

export const readPart = (zip: Unzipped, path: string): Document | null =>
  zip[path] ? parseXml(strFromU8(zip[path])) : null;

/** Parts that are already packed are not packed again: a file with photos saves as fast as one without. */
const PACKED = /\.(?:png|jpe?g|gif|webp|mp4|m4v|mov|mp3|m4a|wdp|zip|bin)$/i;

const serialized = (doc: Document): Uint8Array => {
  const text = new XMLSerializer().serializeToString(doc);
  const head = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n';
  return strToU8(text.startsWith('<?xml') ? text : head + text);
};

export const packParts = (zip: Unzipped, changed: ReadonlyMap<string, Document>): Uint8Array => {
  const parts: Zippable = {};
  for (const [path, bytes] of Object.entries(zip)) {
    const doc = changed.get(path);
    parts[path] = doc ? serialized(doc) : [bytes, { level: PACKED.test(path) ? 0 : 6 }];
  }
  return zipSync(parts, { level: 6 });
};

/** The path of a part another part points at from `from`. */
export const resolvePart = (from: string, target: string): string => {
  if (target.startsWith('/')) return target.slice(1);
  const parts = from.split('/').slice(0, -1);
  for (const piece of target.split('/')) {
    if (piece === '..') parts.pop();
    else if (piece !== '.') parts.push(piece);
  }
  return parts.join('/');
};

export const relsPathOf = (path: string): string => {
  const at = path.lastIndexOf('/');
  return `${path.slice(0, at)}/_rels/${path.slice(at + 1)}.rels`;
};

export interface IRelation {
  target: string;
  type: string;
  external: boolean;
}

export const readRelations = (zip: Unzipped, path: string): Map<string, IRelation> => {
  const out = new Map<string, IRelation>();
  const doc = readPart(zip, relsPathOf(path));
  if (!doc) return out;
  for (const relation of doc.getElementsByTagName('Relationship')) {
    const external = relation.getAttribute('TargetMode') === 'External';
    const target = relation.getAttribute('Target') ?? '';
    out.set(relation.getAttribute('Id') ?? '', {
      target: external ? target : resolvePart(path, target),
      type: relation.getAttribute('Type') ?? '',
      external,
    });
  }
  return out;
};

export const safeHref = (target: string): string | null =>
  /^(https?:|mailto:)/i.test(target) ? target : null;

const MIME: Readonly<Record<string, string>> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
  bmp: 'image/bmp',
  svg: 'image/svg+xml',
};

/** The file's pictures as addresses of the page. */
export class OfficeImages {
  private readonly urls = new Map<string, string>();

  constructor(private readonly zip: Unzipped) {}

  /** Null for a picture a browser cannot draw (old Windows metafiles). */
  urlOf(path: string): string | null {
    const known = this.urls.get(path);
    if (known) return known;
    const bytes = this.zip[path];
    const type = MIME[path.slice(path.lastIndexOf('.') + 1).toLowerCase()];
    if (!bytes || !type) return null;
    const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type }));
    this.urls.set(path, url);
    return url;
  }

  stop(): void {
    for (const url of this.urls.values()) URL.revokeObjectURL(url);
    this.urls.clear();
  }
}
