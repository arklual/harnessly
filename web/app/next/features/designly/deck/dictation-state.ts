// The dictation of notes as a state machine: nothing → listening (the raw words come in) → tidying (the model makes
// notes of them) → nothing. A silence ends it with nothing to tidy. The hook in ./dictation.ts drives it. No React here.

export const DICT = { idle: 'idle', listening: 'listening', tidying: 'tidying' } as const;
export type TDictPhase = (typeof DICT)[keyof typeof DICT];

/** Whose notes are being spoken: one slide's, or the whole deck's (the model splits it by slide). */
export type TDictTarget = { kind: 'slide'; page: string } | { kind: 'all' };

export interface IDictState {
  phase: TDictPhase;
  target: TDictTarget | null;
  /** The words recognized so far. */
  raw: string;
}

export const DICT_IDLE: IDictState = { phase: DICT.idle, target: null, raw: '' };

export type TDictEvent =
  | { k: 'start'; target: TDictTarget }
  | { k: 'words'; raw: string }
  /** The recognition ended with its final text. */
  | { k: 'final'; raw: string }
  /** The recording ended with no words, was cancelled or failed. */
  | { k: 'end' }
  | { k: 'tidied' }
  | { k: 'failed' };

export const dictReduce = (state: IDictState, event: TDictEvent): IDictState => {
  switch (event.k) {
    case 'start':
      return state.phase === DICT.idle ? { phase: DICT.listening, target: event.target, raw: '' } : state;
    case 'words':
      return state.phase === DICT.listening ? { ...state, raw: event.raw } : state;
    case 'final':
      return state.phase === DICT.listening ? { ...state, raw: event.raw, phase: DICT.tidying } : state;
    case 'end':
      return state.phase === DICT.listening ? DICT_IDLE : state;
    case 'tidied':
    case 'failed':
      return state.phase === DICT.tidying ? DICT_IDLE : state;
  }
};

/** Is `page` the slide being dictated (or any, for the whole deck)? */
export const dictatesPage = (state: IDictState, page: string): boolean =>
  state.target !== null &&
  state.phase !== DICT.idle &&
  state.target.kind === 'slide' &&
  state.target.page === page;
