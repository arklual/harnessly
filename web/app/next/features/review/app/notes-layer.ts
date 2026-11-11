// The layer of remarks over a diff's editors: the lines a remark is on are tinted, and under its last line a block
// holds its card. The cards themselves are React's (portals into the blocks the layer hands out), so the layer only
// places blocks, tells who holds which, and re-measures the editor when a card grows. Dragging over the line numbers
// picks the lines of a new remark.
import { StateEffect, StateField, type Extension, type Text } from '@codemirror/state';
import { Decoration, EditorView, ViewPlugin, WidgetType, type DecorationSet } from '@codemirror/view';
import type { TLineSide } from '@/next/features/review/model';

export interface ILayerCard {
  key: string;
  /** The line the card stands under. */
  line: number;
}

export interface ILayerRange {
  from: number;
  to: number;
  /** A kept remark's lines, the ones being picked for a new one, or the lines an agent suggests about. */
  kind: 'note' | 'pick' | 'agent';
}

export interface ILayerMarks {
  cards: readonly ILayerCard[];
  ranges: readonly ILayerRange[];
}

export interface ILayerSlot {
  key: string;
  side: TLineSide;
  el: HTMLElement;
}

type TOnPick = (side: TLineSide, from: number, to: number, done: boolean) => void;

const NO_MARKS: ILayerMarks = { cards: [], ranges: [] };
const CARD_HEIGHT = 72;

const setMarks = StateEffect.define<ILayerMarks>();

class Slot extends WidgetType {
  constructor(
    readonly key: string,
    readonly side: TLineSide,
    readonly layer: NotesLayer,
  ) {
    super();
  }
  eq(other: Slot) {
    return other.key === this.key && other.side === this.side;
  }
  toDOM() {
    const el = document.createElement('div');
    el.className = 'hx-rvnote-slot';
    this.layer.attach({ key: this.key, side: this.side, el });
    return el;
  }
  destroy(el: HTMLElement) {
    this.layer.detach(this.key, this.side, el);
  }
  get estimatedHeight() {
    return this.layer.heightOf(this.key);
  }
  ignoreEvent() {
    return true;
  }
}

const lineAt = (view: EditorView, line: number) =>
  view.state.doc.line(Math.max(1, Math.min(line, view.state.doc.lines)));

/** Who puts marks on the layer: the person's remarks (or a merge request's discussions), and the agent's suggestions. */
export const LAYER_OWNER = { main: 'main', agent: 'agent' } as const;
export type TLayerOwner = (typeof LAYER_OWNER)[keyof typeof LAYER_OWNER];

const joined = (all: readonly ILayerMarks[]): ILayerMarks => ({
  cards: all.flatMap((m) => m.cards),
  ranges: all.flatMap((m) => m.ranges),
});

export class NotesLayer {
  private readonly owned = new Map<TLayerOwner, Record<TLineSide, ILayerMarks>>();
  private marks: Record<TLineSide, ILayerMarks> = { after: NO_MARKS, before: NO_MARKS };
  private readonly views = new Map<EditorView, TLineSide>();
  private slots: readonly ILayerSlot[] = [];
  private readonly heights = new Map<string, number>();
  private readonly listeners = new Set<() => void>();
  private readonly resize =
    typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => this.measure());

  private onPick: TOnPick = () => {};

  /** Hears the lines picked by a drag over the numbers: while dragging, and once more with `done` on release. */
  listenPick(listener: TOnPick): () => void {
    this.onPick = listener;
    return () => {
      if (this.onPick === listener) this.onPick = () => {};
    };
  }

  extension(side: TLineSide): Extension {
    const field = StateField.define<DecorationSet>({
      create: (state) => this.decorate(side, state.doc),
      update: (value, tr) => {
        for (const effect of tr.effects) if (effect.is(setMarks)) return this.decorate(side, tr.state.doc);
        return value.map(tr.changes);
      },
      provide: (f) => EditorView.decorations.from(f),
    });
    const plugin = ViewPlugin.define((view) => {
      this.views.set(view, side);
      let start = 0;
      const lineOf = (event: MouseEvent) =>
        view.state.doc.lineAt(view.lineBlockAtHeight(event.clientY - view.documentTop).from).number;
      const move = (event: MouseEvent) => {
        const at = lineOf(event);
        this.onPick(side, Math.min(start, at), Math.max(start, at), false);
      };
      const up = (event: MouseEvent) => {
        const at = lineOf(event);
        document.removeEventListener('mousemove', move);
        document.removeEventListener('mouseup', up);
        this.onPick(side, Math.min(start, at), Math.max(start, at), true);
      };
      const down = (event: MouseEvent) => {
        const target = event.target as HTMLElement | null;
        if (event.button !== 0 || !target?.closest('.cm-lineNumbers .cm-gutterElement')) return;
        event.preventDefault();
        start = lineOf(event);
        this.onPick(side, start, start, false);
        document.addEventListener('mousemove', move);
        document.addEventListener('mouseup', up);
      };
      view.dom.addEventListener('mousedown', down);
      return {
        destroy: () => {
          view.dom.removeEventListener('mousedown', down);
          document.removeEventListener('mousemove', move);
          document.removeEventListener('mouseup', up);
          this.views.delete(view);
        },
      };
    });
    return [field, plugin];
  }

  private decorate(side: TLineSide, doc: Text): DecorationSet {
    const marks = this.marks[side];
    const clamp = (n: number) => Math.max(1, Math.min(n, doc.lines));
    const decorations = [];
    for (const range of marks.ranges)
      for (let n = clamp(range.from); n <= clamp(range.to); n++)
        decorations.push(
          Decoration.line({ class: `hx-rvnote-line hx-rvnote-line--${range.kind}` }).range(doc.line(n).from),
        );
    for (const card of marks.cards)
      decorations.push(
        Decoration.widget({ widget: new Slot(card.key, side, this), block: true, side: 1 }).range(
          doc.line(clamp(card.line)).to,
        ),
      );
    return Decoration.set(decorations, true);
  }

  /** Puts one owner's marks of a side on its editors, beside the other owners'. */
  set(side: TLineSide, marks: ILayerMarks, owner: TLayerOwner = LAYER_OWNER.main): void {
    const mine = this.owned.get(owner) ?? { after: NO_MARKS, before: NO_MARKS };
    this.owned.set(owner, { ...mine, [side]: marks });
    const all = joined([...this.owned.values()].map((m) => m[side]));
    this.marks = { ...this.marks, [side]: all };
    for (const [view, s] of this.views) if (s === side) view.dispatch({ effects: setMarks.of(all) });
  }

  /** The text of the side's lines, to place remarks by (empty until an editor of the side is drawn). */
  docLines(side: TLineSide): string[] | null {
    const view = [...this.views].find(([, s]) => s === side)?.[0];
    if (!view) return null;
    const doc = view.state.doc;
    const out: string[] = [];
    for (let n = 1; n <= doc.lines; n++) out.push(doc.line(n).text);
    return out;
  }

  /** Scrolls a line of a side to the middle. */
  reveal(side: TLineSide, line: number): void {
    const view = [...this.views].find(([, s]) => s === side)?.[0];
    if (view) view.dispatch({ effects: EditorView.scrollIntoView(lineAt(view, line).from, { y: 'center' }) });
  }

  attach(slot: ILayerSlot): void {
    this.slots = [...this.slots.filter((s) => !(s.key === slot.key && s.side === slot.side)), slot];
    this.resize?.observe(slot.el);
    this.emit();
  }

  detach(key: string, side: TLineSide, el: HTMLElement): void {
    if (el.offsetHeight > 0) this.heights.set(key, el.offsetHeight);
    this.resize?.unobserve(el);
    this.slots = this.slots.filter((s) => s.el !== el);
    this.emit();
  }

  heightOf(key: string): number {
    return this.heights.get(key) ?? CARD_HEIGHT;
  }

  measure(): void {
    for (const view of this.views.keys()) view.requestMeasure();
  }

  // ── for useSyncExternalStore ──
  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
  readonly getSlots = (): readonly ILayerSlot[] => this.slots;
  private emit() {
    // A widget comes and goes inside the editor's own update: React hears of it after.
    queueMicrotask(() => {
      for (const listener of this.listeners) listener();
    });
  }

  /** Called when a new editor is drawn: its side's marks are put on it. */
  refresh(): void {
    for (const [view, side] of this.views) view.dispatch({ effects: setMarks.of(this.marks[side]) });
  }

  dispose(): void {
    this.resize?.disconnect();
    this.listeners.clear();
  }
}
