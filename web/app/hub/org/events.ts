// The organization's live stream (GET /api/orgs/{org}/events) for the screens that show people: when the hub says the
// members or the invitations changed, the screen reads them again. The stream carries no copy of anything, so a lost
// event costs a refetch; with the stream off for the organization (the `org.events` flag) the screens work as before,
// from what they read when opened.
import { useEffect, useRef } from 'react';

import { navigate } from '../lib/router';
import { OrgApiError } from './api/errors';
import { useOrg } from './context';
import { dataMode } from './mode';

/** The kinds that say the people of the organization changed (internal/hub/events/people.go). */
export const PEOPLE_KINDS = ['members.changed', 'invites.changed'] as const;

export interface OrgSource {
  /** 0 connecting, 1 open, 2 closed (EventSource). */
  readonly readyState: number;
  onopen: (() => void) | null;
  onerror: (() => void) | null;
  addEventListener(type: string, fn: () => void): void;
  close(): void;
}

export const orgEventsUrl = (org: string, scopeSlug: string | null): string =>
  `/api/orgs/${encodeURIComponent(org)}/events${scopeSlug ? `?scope=${encodeURIComponent(scopeSlug)}` : ''}`;

export interface FollowOptions {
  url: string;
  kinds: readonly string[];
  /** One of the kinds came, the stream lost events (`resync`), or it opened again after a break. */
  onChange: () => void;
  /** The hub ended a stream that was open and refused the next one: the person may have lost their place here. */
  onLost: () => void;
  source?: (url: string) => OrgSource | null;
}

const CLOSED = 2;

const browserSource = (url: string): OrgSource | null =>
  typeof EventSource === 'undefined'
    ? null
    : (new EventSource(url, { withCredentials: true }) as unknown as OrgSource);

/** Follows the stream until the returned function is called. */
export function followOrgEvents(o: FollowOptions): () => void {
  const es = (o.source ?? browserSource)(o.url);
  if (!es) return () => {};
  let wasOpen = false;
  let done = false;
  for (const kind of [...o.kinds, 'resync']) {
    es.addEventListener(kind, () => {
      if (!done) o.onChange();
    });
  }
  es.onopen = () => {
    if (done) return;
    if (wasOpen) o.onChange();
    wasOpen = true;
  };
  es.onerror = () => {
    if (done || es.readyState !== CLOSED) return;
    // Refused: with the flag off it never opened, and there is nothing to say. After it had been open, the hub ended
    // it (a member removed, a role changed) and did not let the page back in.
    done = true;
    if (wasOpen) o.onLost();
  };
  return () => {
    done = true;
    es.close();
  };
}

/** A person whom the organization no longer lets in is taken to the list of their spaces. */
export const lostTheOrg = (e: unknown): boolean =>
  e instanceof OrgApiError && (e.code === 'not_found' || e.code === 'forbidden');

/** Calls onChange when the members or the invitations of the organization (or of the tenant in view) changed. */
export const useOrgPeople = (onChange: () => void): void => {
  const { api, shell, scope } = useOrg();
  const org = shell.org.slug;
  const scopeSlug = scope.kind === 'tenant' ? scope.tenant.slug : null;
  const changed = useRef(onChange);
  useEffect(() => {
    changed.current = onChange;
  });
  useEffect(() => {
    if (dataMode() === 'mock') return;
    return followOrgEvents({
      url: orgEventsUrl(org, scopeSlug),
      kinds: PEOPLE_KINDS,
      onChange: () => changed.current(),
      onLost: () => {
        api.shell(org, scopeSlug ?? 'all').then(
          () => changed.current(),
          (e: unknown) => {
            if (lostTheOrg(e)) navigate('/');
          },
        );
      },
    });
  }, [api, org, scopeSlug]);
};
