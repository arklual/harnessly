// What the page asks the runtime for the documentation (cmd/runtime/routes_help.go): the sections with their pages, one
// page's Markdown, a search, and the pictures a page shows, in the person's language (a page with no translation comes
// in English). The library is the built-in sections and the machine's own folder (docs/library.go).
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { RuntimeError, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import { t, type TLang } from '@/next/i18n';

export const AUDIENCE = { people: 'people', agents: 'agents', all: 'all' } as const;
export type TAudience = (typeof AUDIENCE)[keyof typeof AUDIENCE];

export interface IGuidePage {
  name: string;
  title: string;
  headings: string[];
  /** The page's file in its source: its links and pictures are relative to it. */
  file: string;
  /** The file actually read: the translation when there is one, else `file`. */
  read: string;
  origin: string;
}

export interface IGuideSection {
  id: string;
  title: string;
  audience: TAudience;
  origin: string;
  pages: IGuidePage[];
}

export interface IGuideHit {
  section: string;
  name: string;
  title: string;
  heading: string;
  snippet: string;
}

/** A failure of the ask; only one that is the network's (or an answer that did not come) is worth asking again. */
export class HelpError extends Error {
  constructor(
    message: string,
    readonly network: boolean,
  ) {
    super(message);
  }
}

const asHelpError = (failure: unknown): HelpError => {
  if (!(failure instanceof RuntimeError)) return new HelpError(t('next.help.failed'), false);
  const lost = [t('next.failure.network'), t('next.failure.timeout')].includes(failure.message);
  return new HelpError(lost ? failure.message : t('next.help.failed'), lost);
};

const ask = async <T>(m: MachineId, path: `/${string}`, signal?: AbortSignal): Promise<T> => {
  try {
    return await runtimeJson<T>(m, platformPath(path), signal ? { signal } : undefined);
  } catch (failure) {
    if (signal?.aborted) throw failure;
    throw asHelpError(failure);
  }
};

export const loadLibrary = async (m: MachineId, lang: TLang): Promise<IGuideSection[]> =>
  (await ask<{ sections: IGuideSection[] }>(m, `/help/library?locale=${lang}`)).sections;

export const loadGuidePage = async (
  m: MachineId,
  lang: TLang,
  section: string,
  name: string,
): Promise<string> => {
  const query = new URLSearchParams({ section, name, locale: lang });
  return (await ask<{ markdown: string }>(m, `/help/library/page?${query}`)).markdown;
};

export const searchGuide = async (
  m: MachineId,
  lang: TLang,
  q: string,
  signal: AbortSignal,
): Promise<IGuideHit[]> => {
  const query = new URLSearchParams({ q, locale: lang });
  return (await ask<{ hits: IGuideHit[] }>(m, `/help/library/search?${query}`, signal)).hits;
};

/** The address of a picture a page shows, `src` as the page writes it; a translation may have pictures of its own. */
export const pictureUrl = (m: MachineId, lang: TLang, section: string, name: string, src: string): string => {
  const query = new URLSearchParams({ section, name, src, locale: lang });
  return runtimePath(platformPath(`/help/library/picture?${query}`), m);
};
