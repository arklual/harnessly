// @vitest-environment jsdom
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { cleanup, fireEvent, render, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DayStage from '@/next/features/day/day';

// The agent's chat of a thing stands for itself: what it is given is what the test reads.
vi.mock('@/next/bindings/chat-view', () => ({
  default: ({ conv, machine, lead }: { conv: string; machine: string; lead?: ReactNode }) => (
    <div className="chat-under-test" data-conv={conv} data-machine={machine}>
      {lead}
    </div>
  ),
}));
const sent = vi.hoisted(() => [] as { machine: string; text: string; conv: string | null }[]);
vi.mock('@/next/commands', async (original) => ({
  ...(await original<typeof import('@/next/commands')>()),
  sendMessage: (machine: string, out: { conversationId: string | null; text: string }) => {
    sent.push({ machine, text: out.text, conv: out.conversationId });
    return 'client';
  },
}));

// The example feed of the repository (examples/ext-day-feed) stands for the feed's site; its dates stand on this day.
const FEED = 'https://example.com/day';
const NOW = Date.parse('2026-03-12T10:40:00Z');
const files = join(__dirname, '..', '..', '..', '..', '..', 'examples', 'ext-day-feed', 'feed');
const file = (name: string) => readFileSync(join(files, name), 'utf8');

interface ICall {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: string;
}

const serve = (over: (call: ICall) => Response | undefined = () => undefined) => {
  const calls: ICall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const call: ICall = {
        url,
        method: init.method ?? 'GET',
        headers: (init.headers ?? {}) as Record<string, string>,
        body: typeof init.body === 'string' ? init.body : '',
      };
      calls.push(call);
      const special = over(call);
      if (special) return special;
      if (call.method === 'POST') return new Response('{"chat":"tab-day-example"}');
      const item = /\/items\/(\d+)(\?|$)/.exec(url);
      return new Response(file(item ? `items/${item[1]}.json` : 'day.json'));
    }),
  );
  return calls;
};

const APP = { id: 'example-corp.day', name: 'Example Corp day' };
// The same feed through the machine's runtime, which signs it in with the app's secret.
const MACHINE_FEED = '/api/platform/apps/feed/example-corp.day/day?machine=m1';

const draw = (feed = FEED, machine: string | null = null) => {
  const greetings: [string, ReactNode][] = [];
  const onGreet = (title: string, sub: ReactNode) => greetings.push([title, sub]);
  const view = render(
    <DayStage app={APP} feed={feed} onGreet={onGreet} clockNow={() => NOW} machine={machine} />,
  );
  return { ...view, greetings };
};

beforeEach(() => {
  sessionStorage.clear();
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('the page of the day from a feed (HRN-1457)', () => {
  it('draws the digest, the calendar and the priorities, and greets by the hour', async () => {
    serve();
    const { container, findByText, greetings } = draw();
    expect(await findByText('Review the export merge request')).toBeTruthy();
    expect(container.querySelector('.hx-day-sum')?.textContent).toContain('Overnight the agent read');
    expect([...container.querySelectorAll('.hx-day-e b')].map((el) => el.textContent)).toEqual(
      expect.arrayContaining(['Standup', 'Planning', 'Focus', 'Venue call', 'Numbers with Lee']),
    );
    // Planning and the design sync share a time: one stack.
    expect(container.querySelectorAll('.hx-day-e[data-stack]')).toHaveLength(1);
    expect(container.querySelector('.hx-day-e[data-offer]')?.textContent).toContain('Numbers with Lee');
    expect(container.querySelector('.hx-day-k__now')?.getAttribute('data-t')).toBe('10:40');
    expect(container.querySelectorAll('.hx-day-pr')).toHaveLength(4);
    await waitFor(() => expect(greetings.at(-1)?.[0]).toBe('Good morning, Alex'));
  });

  it('opens a thing into its chat and sends what the person decided', async () => {
    const calls = serve();
    const opened: string[] = [];
    const onOpen = (event: Event) => opened.push((event as CustomEvent<string>).detail);
    window.addEventListener('hl:open-conversation', onOpen);
    const { container, findByText, getByText } = draw();
    fireEvent.click(await findByText('Board numbers for the quarter'));
    expect(await findByText('Churn, two sources')).toBeTruthy();
    expect(container.querySelector('.hx-day-item')?.getAttribute('data-state')).toBe('open');
    expect(container.querySelector('.hx-day-chip[data-main]')?.textContent).toBe('Count paused as churn');

    fireEvent.click(getByText('Count paused as churn'));
    await waitFor(() => expect(opened).toEqual(['tab-day-example']));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.url).toBe(`${FEED}/items/103/ask`);
    expect(post?.headers['X-Requested-With']).toBe('harnessly');
    expect(JSON.parse(post?.body ?? '{}')).toEqual({
      say: 'Count the paused accounts as churn and redraw the fifth slide.',
    });
    window.removeEventListener('hl:open-conversation', onOpen);
  });

  it("unfolds the agent's chat under the thing, in place, and says the next words into it", async () => {
    const calls = serve();
    const opened: string[] = [];
    const onOpen = (event: Event) => opened.push((event as CustomEvent<string>).detail);
    window.addEventListener('hl:open-conversation', onOpen);
    sent.length = 0;
    const { container, findByText, getByText } = draw(FEED, 'm1');
    fireEvent.click(await findByText('Board numbers for the quarter'));
    expect(await findByText('Churn, two sources')).toBeTruthy();
    expect(container.querySelector('.chat-under-test')).toBeNull();

    fireEvent.click(getByText('Count paused as churn'));
    await waitFor(() => expect(container.querySelector('.chat-under-test')).not.toBeNull());
    const chat = container.querySelector('.chat-under-test');
    expect(chat?.getAttribute('data-conv')).toBe('tab-day-example');
    expect(chat?.getAttribute('data-machine')).toBe('m1');
    // The widget heads the chat, and the page did not leave for the chat elsewhere.
    expect(chat?.querySelector('.hx-day-w')?.textContent).toContain('Churn, two sources');
    expect(container.querySelector('.hx-day-item')?.hasAttribute('data-chat')).toBe(true);
    expect(opened).toEqual([]);

    // The next answer goes into that chat, not into a new one of the feed.
    const asks = calls.filter((c) => c.method === 'POST').length;
    const chips = [...container.querySelectorAll<HTMLButtonElement>('.hx-day-w__f .hx-day-chip')];
    const next = chips.find((b) => b.textContent !== 'Count paused as churn' && !b.disabled);
    expect(next).toBeTruthy();
    if (!next) return;
    fireEvent.click(next);
    if (container.querySelector('.hx-day-w__inp')) fireEvent.submit(container.querySelector('form')!);
    await waitFor(() =>
      expect(sent.length + calls.filter((c) => c.method === 'POST').length).toBeGreaterThan(asks),
    );
    window.removeEventListener('hl:open-conversation', onOpen);
  });

  it('takes the words of the person for an answer of their own', async () => {
    const calls = serve();
    const { container, findByText, getByText } = draw();
    fireEvent.click(await findByText('Answer Dana about the venue'));
    fireEvent.click(await findByText('Change the answer'));
    const field = container.querySelector<HTMLInputElement>('.hx-day-w__inp');
    expect(field).not.toBeNull();
    if (!field) return;
    fireEvent.change(field, { target: { value: 'Ask for forty-two seats' } });
    fireEvent.click(getByText('Send'));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.url).toBe(`${FEED}/items/102/ask`);
    expect(JSON.parse(post?.body ?? '{}')).toEqual({ say: 'Ask for forty-two seats' });
  });

  it('offers to sign in when the feed does not know the person, and asks again after', async () => {
    let signedIn = false;
    serve(() => (signedIn ? undefined : new Response('sign in', { status: 401 })));
    const popup = { closed: false };
    const open = vi.fn(() => popup);
    vi.stubGlobal('open', open);
    const { findByText, container } = draw();
    fireEvent.click(await findByText('Sign in'));
    expect(open).toHaveBeenCalledWith(
      'https://example.com/auth/login?popup=1',
      'hl-day-login',
      expect.any(String),
    );
    expect(container.querySelector('.hx-day-pr')).toBeNull();
    signedIn = true;
    popup.closed = true;
    expect(await findByText('Review the export merge request', undefined, { timeout: 3000 })).toBeTruthy();
  });

  it('says so when the feed does not answer, and tries again on a press', async () => {
    let broken = true;
    serve(() => (broken ? new Response('down', { status: 502 }) : undefined));
    const { findByText } = draw();
    const retry = await findByText('Retry');
    expect(await findByText('The day did not load')).toBeTruthy();
    broken = false;
    fireEvent.click(retry);
    expect(await findByText('Review the export merge request')).toBeTruthy();
  });

  it('reads the day through the machine when the app keeps a token for the feed', async () => {
    const calls = serve();
    const { findByText, getByText } = draw(MACHINE_FEED);
    fireEvent.click(await findByText('Review the export merge request'));
    fireEvent.click(await findByText('Send the comments to Robin'));
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true));
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual(
      expect.arrayContaining([
        `GET ${MACHINE_FEED}`,
        'GET /api/platform/apps/feed/example-corp.day/day/items/101?machine=m1',
        'POST /api/platform/apps/feed/example-corp.day/day/items/101/take?machine=m1',
      ]),
    );
    expect(getByText('Back')).toBeTruthy();
  });

  it('asks for the token in the card of the app, not for a sign-in, when the machine has none', async () => {
    let answer: Response | undefined = new Response('{"error":"secret-missing","secret":"token"}', {
      status: 409,
    });
    serve(() => answer?.clone());
    const open = vi.fn();
    vi.stubGlobal('open', open);
    const { findByText, queryByText, getByText } = draw(MACHINE_FEED);
    expect(await findByText(/Example Corp day needs its token/)).toBeTruthy();
    expect(queryByText('Sign in')).toBeNull();
    fireEvent.click(getByText("Open the app's card"));
    expect(window.location.pathname).toBe('/apps/example-corp.day/about');
    expect(open).not.toHaveBeenCalled();

    // The feed refused the token the machine has: the same place, other words.
    answer = new Response('{"error":"secret-refused","secret":"token"}', { status: 409 });
    fireEvent.click(getByText('Retry'));
    expect(await findByText(/did not take the token of Example Corp day/)).toBeTruthy();

    // The token is put: the day comes on the next ask.
    answer = undefined;
    fireEvent.click(getByText('Retry'));
    expect(await findByText('Review the export merge request')).toBeTruthy();
  });

  it('shows no sign-in of the feed for a lost session of the host', async () => {
    serve(() => new Response('{"error":"Unauthorized"}', { status: 401 }));
    const { findByText, queryByText } = draw(MACHINE_FEED);
    expect(await findByText('The day did not load')).toBeTruthy();
    expect(queryByText('Sign in')).toBeNull();
  });
});
