import { describe, expect, it } from 'vitest';
import { actBlocked, actHost, readAgentAct } from '@/next/features/pane/agent-acts';
import { readFrameMessage } from '@/next/features/pane/frame-bridge';

const act = {
  id: '6b1f0f3e-1',
  op: 'click',
  ref: 'e12',
  element: ' кнопка\n«Купить» ',
  text: '',
  key: '',
  value: '',
  url: '',
  dy: 0,
  ms: 0,
  submit: false,
  event: '',
  tool: '',
  input: '',
};

describe("the agent's action from the machine", () => {
  it('is read field by field, its words on one line', () => {
    expect(readAgentAct(act)).toEqual({ ...act, element: 'кнопка «Купить»' });
  });

  it('drops an unknown op, a bad ref or id, and clamps the numbers', () => {
    expect(readAgentAct({ ...act, op: 'eval' })).toBeNull();
    expect(readAgentAct({ ...act, ref: 'body > a' })).toBeNull();
    expect(readAgentAct({ ...act, id: '' })).toBeNull();
    expect(readAgentAct({ ...act, id: 'x'.repeat(65) })).toBeNull();
    expect(readAgentAct('click')).toBeNull();
    expect(readAgentAct({ ...act, op: 'scroll', ref: '', dy: 1e9 })?.dy).toBe(20000);
    expect(readAgentAct({ ...act, op: 'wait', ms: -5 })?.ms).toBe(0);
  });

  it("carries a video's event, gives such a wait more time, and reads the page", () => {
    expect(readAgentAct({ ...act, op: 'wait', event: 'ended', ms: 20000 })).toMatchObject({
      event: 'ended',
      ms: 20000,
    });
    expect(readAgentAct({ ...act, op: 'wait', ms: 20000 })?.ms).toBe(10000);
    expect(readAgentAct({ ...act, op: 'wait', event: 'stopped' })).toBeNull();
    expect(readAgentAct({ ...act, op: 'read', ref: 'e2' })).toMatchObject({
      op: 'read',
      ref: 'e2',
      event: '',
    });
  });

  it("carries a site tool's name and arguments, and drops a bad name or too long arguments", () => {
    const call = { ...act, op: 'call', ref: '', tool: 'add_to_cart', input: '{"id":"e-12"}' };
    expect(readAgentAct(call)).toMatchObject({ op: 'call', tool: 'add_to_cart', input: '{"id":"e-12"}' });
    expect(readAgentAct({ ...call, tool: 'add to cart' })).toBeNull();
    expect(readAgentAct({ ...call, input: 'x'.repeat(8001) })).toBeNull();
    expect(readAgentAct({ ...act, op: 'tools', ref: '' })?.op).toBe('tools');
  });
});

describe('whether the agent may act on a site', () => {
  it('refuses the sites the person blocked, and only those', () => {
    const blocked = ['b.example.com'];
    expect(actBlocked({ blocked }, 'a.example.com')).toBe(false);
    expect(actBlocked({ blocked }, 'b.example.com')).toBe(true);
    expect(actBlocked({ blocked: [] }, 'b.example.com')).toBe(false);
  });

  it('is about the host of an http(s) page', () => {
    expect(actHost('https://stand-1.preview.example.com/events?x=1')).toBe('stand-1.preview.example.com');
    expect(actHost('http://localhost:4200/')).toBe('localhost');
    expect(actHost('data:text/html,x')).toBeNull();
    expect(actHost(null)).toBeNull();
  });
});

describe("the frame's answer", () => {
  it('carries the outcome and the box of the element', () => {
    expect(
      readFrameMessage({
        type: 'tk:acted',
        id: 'a1',
        ok: true,
        text: 'clicked button "Купить"',
        rect: { x: 1, y: 2, width: 3, height: 4 },
      }),
    ).toEqual({
      k: 'acted',
      acted: {
        id: 'a1',
        ok: true,
        text: 'clicked button "Купить"',
        rect: { x: 1, y: 2, width: 3, height: 4 },
      },
    });
    expect(readFrameMessage({ type: 'tk:acted', id: 'a1', ok: 'yes', text: '' })).toBeNull();
    expect(readFrameMessage({ type: 'tk:acted', id: 'a1', ok: false, text: 'gone', rect: { x: 1 } })).toEqual(
      {
        k: 'acted',
        acted: { id: 'a1', ok: false, text: 'gone', rect: null },
      },
    );
  });
});
