// Shapes of what the runtime answers; the runtime itself is Go.

/** `rules`: the file of rules an agent reads before editing what was picked (a design of the library). */
export type TMatchKind = 'component' | 'template' | 'rules';

export interface ISearchHit {
  file: string;
  line: number;
}

export interface IResolvedMatch extends ISearchHit {
  kind: TMatchKind;
}
