import { describe, expect, it } from 'vitest';

import { OrgApiError } from './api/errors';
import { followOrgEvents, lostTheOrg, orgEventsUrl, PEOPLE_KINDS, type OrgSource } from './events';

class FakeSource implements OrgSource {
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private listeners = new Map<string, (() => void)[]>();
  addEventListener(type: string, fn: () => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  close(): void {
    this.closed = true;
  }
  opened(): void {
    this.readyState = 1;
    this.onopen?.();
  }
  emit(type: string): void {
    for (const fn of this.listeners.get(type) ?? []) fn();
  }
  refused(): void {
    this.readyState = 2;
    this.onerror?.();
  }
  dropped(): void {
    this.readyState = 0;
    this.onerror?.();
  }
}

const follow = () => {
  const es = new FakeSource();
  const seen = { changed: 0, lost: 0 };
  const stop = followOrgEvents({
    url: orgEventsUrl('acme', null),
    kinds: PEOPLE_KINDS,
    onChange: () => void (seen.changed += 1),
    onLost: () => void (seen.lost += 1),
    source: () => es,
  });
  return { es, seen, stop };
};

describe('the organization stream of the people screens', () => {
  it('addresses the organization or one tenant of it', () => {
    expect(orgEventsUrl('acme', null)).toBe('/api/orgs/acme/events');
    expect(orgEventsUrl('acme', 'product')).toBe('/api/orgs/acme/events?scope=product');
  });

  it('reads again when the members or the invitations changed, and when events were lost', () => {
    const { es, seen } = follow();
    es.opened();
    expect(seen.changed).toBe(0);
    es.emit('members.changed');
    es.emit('invites.changed');
    es.emit('resync');
    es.emit('usage.tick');
    expect(seen.changed).toBe(3);
  });

  it('reads again when the stream comes back after a break', () => {
    const { es, seen } = follow();
    es.opened();
    es.dropped();
    es.opened();
    expect(seen).toEqual({ changed: 1, lost: 0 });
  });

  it('a stream the hub ended and did not open again says the person may be out', () => {
    const { es, seen } = follow();
    es.opened();
    es.refused();
    expect(seen.lost).toBe(1);
    es.emit('members.changed');
    expect(seen.changed).toBe(0);
  });

  it('a stream that never opened (the flag is off) says nothing', () => {
    const { es, seen } = follow();
    es.refused();
    expect(seen).toEqual({ changed: 0, lost: 0 });
  });

  it('stops', () => {
    const { es, seen, stop } = follow();
    es.opened();
    stop();
    expect(es.closed).toBe(true);
    es.emit('members.changed');
    expect(seen.changed).toBe(0);
  });

  it('only a refusal of the organization itself takes the person away', () => {
    expect(lostTheOrg(new OrgApiError('not_found', ''))).toBe(true);
    expect(lostTheOrg(new OrgApiError('forbidden', ''))).toBe(true);
    expect(lostTheOrg(new OrgApiError('network', ''))).toBe(false);
    expect(lostTheOrg(new Error('x'))).toBe(false);
  });
});
