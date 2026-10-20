import { useSyncExternalStore } from 'react';
import type { IPickedComponent, TFramework } from '@/lib/hl-preview/bridge-core';
import type { IResolvedMatch } from '@/types/server/resolve';
import type { INewTextAnnotation, ITextAnnotation } from '@/lib/hl-text-annotation';

/** The element as the bridge picked it; `path` is inside the target, `viewport` is the frame's at the pick. */
export interface IAnnotatedElement {
  selector: string;
  tag: string;
  text: string;
  outerHTML: string;
  component: IPickedComponent | null;
  framework: TFramework;
  path: string;
  viewport: { w: number; h: number };
}

/**
 * `file:line` arrives after the chip: none without a component or on an external site (no worktree to search),
 * failed when the runtime could not answer.
 */
export type TAnnotationResolve =
  | { status: 'none' }
  | { status: 'pending' }
  | { status: 'done'; matches: IResolvedMatch[] }
  | { status: 'failed' };

export interface INewAnnotation {
  comment: string;
  /** A dev server of the machine or an external site. */
  target: { port: number } | { origin: string };
  element: IAnnotatedElement;
}

export interface IAnnotation extends INewAnnotation {
  id: string;
  /** Number shown on the chip and in the message: 1, 2, … within the tab, from 1 again after a clear. */
  n: number;
  resolve: TAnnotationResolve;
}

/** What waits in the composer: an element picked in the preview or a fragment selected in the timeline. */
export type TComposerAnnotation = IAnnotation | ITextAnnotation;

export const isTextAnnotation = (annotation: TComposerAnnotation): annotation is ITextAnnotation =>
  'quote' in annotation;

const EMPTY: readonly TComposerAnnotation[] = [];

/**
 * Annotations waiting in the composer of a conversation tab, outside React: the preview adds them, the
 * composer shows them as chips and clears them after a successful send.
 */
export const createAnnotationsStore = () => {
  const byTab = new Map<string, readonly TComposerAnnotation[]>();
  const listeners = new Set<() => void>();
  let ids = 0;
  const notify = () => {
    for (const listener of listeners) listener();
  };
  const set = (tab: string, list: readonly TComposerAnnotation[]) => {
    if (list.length === 0) byTab.delete(tab);
    else byTab.set(tab, list);
    notify();
  };
  const get = (tab: string): readonly TComposerAnnotation[] => byTab.get(tab) ?? EMPTY;
  const nextNumber = (list: readonly TComposerAnnotation[]) => (list.at(-1)?.n ?? 0) + 1;
  return {
    get,
    add: (tab: string, annotation: INewAnnotation): IAnnotation => {
      const list = get(tab);
      const added: IAnnotation = {
        ...annotation,
        id: `a${++ids}`,
        n: nextNumber(list),
        resolve:
          annotation.element.component && 'port' in annotation.target
            ? { status: 'pending' }
            : { status: 'none' },
      };
      set(tab, [...list, added]);
      return added;
    },
    addText: (tab: string, annotation: INewTextAnnotation): ITextAnnotation => {
      const list = get(tab);
      const added: ITextAnnotation = { ...annotation, id: `a${++ids}`, n: nextNumber(list) };
      set(tab, [...list, added]);
      return added;
    },
    update: (tab: string, id: string, patch: { comment: string }): void => {
      const list = get(tab);
      if (!list.some((annotation) => annotation.id === id)) return;
      set(
        tab,
        list.map((annotation) => (annotation.id === id ? { ...annotation, ...patch } : annotation)),
      );
    },
    remove: (tab: string, id: string): void =>
      set(
        tab,
        get(tab).filter((annotation) => annotation.id !== id),
      ),
    clear: (tab: string): void => set(tab, EMPTY),
    /** The lookup of `file:line` finished; null means it failed. A removed annotation stays removed. */
    resolved: (tab: string, id: string, matches: IResolvedMatch[] | null): void => {
      const list = get(tab);
      if (!list.some((annotation) => annotation.id === id && !isTextAnnotation(annotation))) return;
      const resolve: TAnnotationResolve = matches ? { status: 'done', matches } : { status: 'failed' };
      set(
        tab,
        list.map((annotation) => (annotation.id === id ? { ...annotation, resolve } : annotation)),
      );
    },
    subscribe: (listener: () => void): (() => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

const store = createAnnotationsStore();

export const readAnnotations = (tab: string): readonly TComposerAnnotation[] => store.get(tab);
export const addAnnotation = (tab: string, annotation: INewAnnotation): IAnnotation =>
  store.add(tab, annotation);
export const addTextAnnotation = (tab: string, annotation: INewTextAnnotation): ITextAnnotation =>
  store.addText(tab, annotation);
export const updateAnnotationComment = (tab: string, id: string, comment: string): void =>
  store.update(tab, id, { comment });
export const removeAnnotation = (tab: string, id: string): void => store.remove(tab, id);
export const clearAnnotations = (tab: string): void => store.clear(tab);
export const annotationResolved = (tab: string, id: string, matches: IResolvedMatch[] | null): void =>
  store.resolved(tab, id, matches);

export const useAnnotations = (tab: string): readonly TComposerAnnotation[] =>
  useSyncExternalStore(
    store.subscribe,
    () => store.get(tab),
    () => EMPTY,
  );
