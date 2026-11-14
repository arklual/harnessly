// The one answer to «which icon is this Sinopia Design project»: by its kind (site and boards, slides, video, design
// system), a document of an app's type, or a project whose kind is not known. ⌘K, the sidebar, the library, its trash,
// the tabs and the made list all draw it from here, so a kind looks the same everywhere.
import type { DesignKind } from '@/next/protocol/v2';

export const DESIGN_KIND_ICON = {
  site: 'app-window',
  slides: 'presentation',
  video: 'clapperboard',
  ds: 'palette',
} as const satisfies Record<DesignKind, string>;

/** A document of a type an installed app adds to Sinopia. */
export const DESIGN_DOC_ICON = 'file-text';
/** A project whose kind this page does not know. */
export const DESIGN_ANY_ICON = 'pen-tool';

interface IDesignLook {
  kind?: DesignKind | null;
  appKind?: unknown;
}

export const designIcon = ({ kind, appKind }: IDesignLook): string => {
  if (appKind) return DESIGN_DOC_ICON;
  return kind ? DESIGN_KIND_ICON[kind] : DESIGN_ANY_ICON;
};
