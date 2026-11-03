// What the page asks the runtime for the documentation (cmd/runtime/routes_help.go): the list of pages, and one page's
// Markdown, in the person's language (a page with no translation comes in English).
import { platformPath } from '@/next/core/net/machine';
import { RuntimeError, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import { t, type TLang } from '@/next/i18n';

export interface IGuidePage {
  name: string;
  title: string;
  headings: string[];
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

export const loadGuide = async (m: MachineId, lang: TLang): Promise<IGuidePage[]> => {
  try {
    const out = await runtimeJson<{ pages: IGuidePage[] }>(m, platformPath(`/help/guide?locale=${lang}`));
    return out.pages;
  } catch (failure) {
    throw asHelpError(failure);
  }
};

export const loadGuidePage = async (m: MachineId, lang: TLang, name: string): Promise<string> => {
  try {
    const query = new URLSearchParams({ name, locale: lang });
    const out = await runtimeJson<{ markdown: string }>(m, platformPath(`/help/guide/page?${query}`));
    return out.markdown;
  } catch (failure) {
    throw asHelpError(failure);
  }
};
