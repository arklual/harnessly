// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import * as core from './bridge-core';

/** An element with its scroll box and overflow, as `scrollsBy` reads them. */
const scroller = (
  overflow: string,
  box: { top: number; client: number; height: number },
  parent: unknown = null,
) => ({
  nodeType: 1,
  parentElement: parent,
  overflow,
  scrollTop: box.top,
  clientHeight: box.client,
  scrollHeight: box.height,
  scrollLeft: 0,
  clientWidth: 100,
  scrollWidth: 100,
});

const pageOf = (rootOverflow: string, root: { top: number; client: number; height: number }) => {
  const html = scroller(rootOverflow, root);
  const body = scroller('visible', { top: 0, client: 0, height: 0 }, html);
  const doc = { scrollingElement: html, documentElement: html, body };
  const win = {
    getComputedStyle: (el: { overflow: string }) => ({ overflowX: el.overflow, overflowY: el.overflow }),
  };
  return { win, doc, html, body };
};

describe('the bridge on a canvas', () => {
  it('knows a wheel something under the pointer scrolls by', () => {
    const { win, doc, body } = pageOf('visible', { top: 0, client: 500, height: 500 });
    const list = scroller('auto', { top: 0, client: 100, height: 300 }, body);
    const item = scroller('visible', { top: 0, client: 20, height: 20 }, list);
    expect(core.scrollsBy(win, doc, item, 0, 40)).toBe(true);
    // At its top it cannot go up, and the page has no room: the wheel is the canvas's.
    expect(core.scrollsBy(win, doc, item, 0, -40)).toBe(false);
    const hidden = scroller('hidden', { top: 0, client: 100, height: 300 }, body);
    expect(core.scrollsBy(win, doc, hidden, 0, 40)).toBe(false);
  });

  it('lets the document scroll unless its overflow is hidden', () => {
    const open = pageOf('visible', { top: 0, client: 500, height: 900 });
    expect(core.scrollsBy(open.win, open.doc, open.body, 0, 40)).toBe(true);
    const shut = pageOf('hidden', { top: 0, client: 500, height: 900 });
    expect(core.scrollsBy(shut.win, shut.doc, shut.body, 0, 40)).toBe(false);
  });

  it('leaves Space to a field being typed in', () => {
    const input = document.createElement('input');
    const button = document.createElement('button');
    const edit = document.createElement('div');
    edit.setAttribute('contenteditable', 'true');
    expect(core.isTypingTarget(input)).toBe(true);
    expect(core.isTypingTarget(edit)).toBe(true);
    expect(core.isTypingTarget(button)).toBe(false);
    expect(core.isTypingTarget(document.body)).toBe(false);
  });

  it('reads the parent turning the canvas on', () => {
    expect(core.parseParentMessage({ type: 'tk:canvas', on: true })).toEqual({ type: 'tk:canvas', on: true });
    expect(core.parseParentMessage({ type: 'tk:canvas', on: 'yes' })).toBeNull();
  });
});
