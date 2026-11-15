// The kinds of a design, apart from the library's page so the shell names them without loading it.
export const DESIGN_KIND = { site: 'site', slides: 'slides', video: 'video', ds: 'ds' } as const;
export type TDesignKind = (typeof DESIGN_KIND)[keyof typeof DESIGN_KIND];
