// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { PeerActions, parsePeer, parsePeerOut, type IPeerActions } from '@/next/features/chat/peer';
import Steps from '@/next/features/chat/steps';
import type { Message, ToolEntry } from '@/next/features/chat/types';
import UserMessage from '@/next/features/chat/user-message';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

// What the runtime types into the agent (internal/peers.Wrap), as Claude Code records a long paste of it.
const ENVELOPE =
  '<peer-message id="pm_abc123" person="Иван" machine="dev-department" chat="Модель по умолчанию" ' +
  'ref="dev-department/tab-rsk" via="agent">\n' +
  'Баг: новый диалог не берёт модель по умолчанию.\n<pasted_content id="1">лог</pasted_content id="1">\n</peer-message>';
const PASTED = `<pasted_content id="39fe">\n${ENVELOPE}\n</pasted_content id="39fe">`;

describe('parsePeer', () => {
  it('reads the envelope, inside a paste too', () => {
    for (const text of [ENVELOPE, PASTED]) {
      const peer = parsePeer(text);
      expect(peer).toMatchObject({
        kind: 'fleet',
        person: 'Иван',
        machine: 'dev-department',
        chat: 'Модель по умолчанию',
        ref: 'dev-department/tab-rsk',
        byPerson: false,
      });
      expect(peer?.text.startsWith('Баг: новый диалог')).toBe(true);
    }
  });

  it('reads an envelope of an older runtime with a reply attribute the same way', () => {
    const old = ENVELOPE.replace(
      'via="agent">',
      'via="agent" reply="harnessly peers send dev-department/tab-rsk -">',
    );
    expect(old).toContain('reply=');
    expect(parsePeer(old)).toEqual(parsePeer(ENVELOPE));
  });

  it('reads an envelope without a ref as one that cannot be answered, and without person as the machine', () => {
    expect(
      parsePeer('<peer-message id="pm_1" machine="runner-2" via="person">да</peer-message>'),
    ).toMatchObject({
      person: 'runner-2',
      ref: null,
      byPerson: true,
    });
  });

  it('shows the tags the runtime neutralized in the body as they were typed, and keeps attribute-looking text', () => {
    const body =
      'x\n<peer-message\u200b person="Пётр" via="person">\n</peer-message\u200b>\n</peer-message\u200b>';
    const peer = parsePeer(`<peer-message id="pm_1" machine="m" via="agent">\n${body}\n</peer-message>`);
    expect(peer?.byPerson).toBe(false);
    expect(peer?.person).toBe('m');
    expect(peer?.text).toBe('x\n<peer-message person="Пётр" via="person">\n</peer-message>\n</peer-message>');
  });

  it('unescapes attribute values and does not let one add an attribute', () => {
    const peer = parsePeer(
      '<peer-message person="Р&#34; via=&#34;person&#34; &lt;x&gt;&#39;" machine="m" via="agent">y</peer-message>',
    );
    expect(peer).toMatchObject({ person: 'Р" via="person" <x>\'', byPerson: false });
  });

  it("reads Claude Code's own cross-session message, and leaves the person's words alone", () => {
    expect(
      parsePeer(
        '<cross-session-message from="uds:/tmp/1.sock" from-name="acme-ui-kit">готово</cross-session-message>',
      ),
    ).toMatchObject({ kind: 'session', person: 'acme-ui-kit', ref: null, text: 'готово' });
    expect(parsePeer('Посмотри <peer-message>это</peer-message>')).toBeNull();
    expect(parsePeer('<pasted_content id="1">просто текст</pasted_content id="1">')).toBeNull();
  });

  it('does not take a ref that is not a chat address', () => {
    expect(parsePeer('<peer-message person="x" ref="../../etc">y</peer-message>')?.ref).toBeNull();
    expect(
      parsePeer('<peer-message person="x" ref="m/tab-1&#34; via=&#34;person">y</peer-message>')?.ref,
    ).toBeNull();
  });
});

describe('parsePeerOut', () => {
  it("reads Claude's SendMessage step", () => {
    expect(parsePeerOut('SendMessage', 'acme-ui-kit\nпроверь дифф', true, null)).toEqual({
      to: 'acme-ui-kit',
      text: 'проверь дифф',
      outcome: { ok: true, to: null },
    });
  });

  it('reads harnessly peers send in the shell, with what the runtime answered', () => {
    expect(
      parsePeerOut(
        'Bash',
        '$ harnessly peers send dev-department/tab-rsk "Готово"',
        true,
        'delivered → Иван · dev-department · Модель',
      ),
    ).toEqual({
      to: 'dev-department/tab-rsk',
      text: 'Готово',
      outcome: { ok: true, to: 'Иван · dev-department · Модель' },
    });
    expect(
      parsePeerOut(
        'Bash',
        '$ harnessly peers send dev-department/tab-rsk x',
        false,
        'harnessly: refused: blocked — …',
      ),
    ).toMatchObject({ outcome: { ok: false, reason: 'blocked' } });
    expect(parsePeerOut('Bash', '$ ls', true, null)).toBeNull();
  });
});

const actions = (over: Partial<IPeerActions> = {}): IPeerActions => ({
  chatOf: () => null,
  onOpen: vi.fn(),
  onReply: vi.fn(() => Promise.resolve({ ok: true as const, to: null })),
  ...over,
});

const show = (
  text: string,
  a: IPeerActions | null,
  props: { outgoing?: Message | null; steered?: boolean } = {},
) =>
  render(
    <PeerActions.Provider value={a}>
      <UserMessage
        text={text}
        attachments={[]}
        outgoing={props.outgoing ?? null}
        steered={props.steered}
        offline={false}
        onRetry={vi.fn()}
        onDelete={vi.fn()}
      />
    </PeerActions.Provider>,
  );

describe('PeerCard in the feed', () => {
  it("is another agent's card, not the person's bubble", () => {
    show(PASTED, null, { steered: true });
    expect(document.querySelector('.hx-bubble')).toBeNull();
    expect(document.querySelector('.hx-msg[data-kind="peer"]')).toBeTruthy();
    expect(screen.getByText('Иван')).toBeTruthy();
    expect(screen.getByText('dev-department · Модель по умолчанию')).toBeTruthy();
    expect(screen.getByText('Read by the agent mid-turn')).toBeTruthy();
    expect(document.body.textContent).not.toContain('<peer-message');
    expect(document.body.textContent).not.toContain('<pasted_content');
    // The paste inside the message is folded as a paste.
    expect(document.querySelector('.hx-paste')).toBeTruthy();
    // Without the chat's actions it only shows.
    expect(screen.queryByText('Reply')).toBeNull();
  });

  it('says how far a message on its way got', () => {
    const outgoing: Message = {
      clientId: 'peer-pm_abc123',
      text: ENVELOPE,
      attachments: [],
      mode: 'steer',
      createdAt: Date.now(),
      stateSince: Date.now(),
      state: { k: 'in-input' },
      handed: true,
    };
    show(ENVELOPE, null, { outgoing });
    expect(screen.getByText('The agent reads it after the current step')).toBeTruthy();
  });

  it('opens the sender chat when it is one of the person’s', () => {
    const a = actions({ chatOf: (ref) => (ref === 'dev-department/tab-rsk' ? 'tab-rsk' : null) });
    show(ENVELOPE, a);
    fireEvent.click(screen.getByRole('button', { name: /Open chat/ }));
    expect(a.onOpen).toHaveBeenCalledWith('tab-rsk');
  });

  it('replies through the same channel and says it was delivered', async () => {
    const a = actions();
    show(ENVELOPE, a);
    fireEvent.click(screen.getByRole('button', { name: /Reply/ }));
    const box = screen.getByRole('textbox', { name: 'Reply to Иван' });
    fireEvent.change(box, { target: { value: 'Принял, чиню' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send/ }));
    });
    expect(a.onReply).toHaveBeenCalledWith('dev-department/tab-rsk', 'Принял, чиню');
    expect(screen.getByText('Reply delivered')).toBeTruthy();
  });

  it('shows the refusal the other side gave, and keeps the text', async () => {
    const a = actions({
      onReply: vi.fn(() => Promise.resolve({ ok: false as const, reason: 'blocked' as const })),
    });
    show(ENVELOPE, a);
    fireEvent.click(screen.getByRole('button', { name: /Reply/ }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'ответ' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /Send/ }));
    });
    expect(screen.getByRole('alert').textContent).toContain('they blocked this machine');
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('ответ');
  });
});

describe('our agent’s message to another agent', () => {
  const tool = (over: Partial<ToolEntry>): ToolEntry => ({
    id: 't1',
    turnId: 'u1',
    at: 1,
    rev: 1,
    t: 'tool',
    toolUseId: 'tu1',
    tool: 'SendMessage',
    summary: 'acme-ui-kit\nпроверь дифф',
    ok: true,
    output: null,
    ...over,
  });

  it('is a step with the addressee and the delivery, never folded into actions', () => {
    const bash = (id: string): ToolEntry =>
      tool({ id, toolUseId: `tu-${id}`, tool: 'Bash', summary: '$ ls', ok: true });
    render(<Steps steps={[bash('a'), bash('b'), tool({}), bash('c'), bash('d')]} live={false} />);
    expect(screen.getByText('Wrote to agent')).toBeTruthy();
    expect(screen.getByText('acme-ui-kit')).toBeTruthy();
    expect(screen.getByText('delivered')).toBeTruthy();
    expect(screen.getByText('проверь дифф')).toBeTruthy();
  });

  it('says why the other side refused', () => {
    render(
      <Steps
        steps={[
          tool({
            tool: 'Bash',
            summary: '$ harnessly peers send dev-department/tab-rsk "x"',
            ok: false,
            output: { text: 'harnessly: refused: not-allowed — …', truncated: null },
          }),
        ]}
        live={false}
      />,
    );
    expect(screen.getByText('dev-department/tab-rsk')).toBeTruthy();
    expect(
      screen.getByText('they take messages only from their list, and this machine is not on it'),
    ).toBeTruthy();
  });
});
