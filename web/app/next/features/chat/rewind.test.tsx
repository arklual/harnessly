// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  RewindActions,
  RewindablePrompt,
  RewoundRow,
  type IRewindActions,
} from '@/next/features/chat/rewind';
import type { RewoundItem } from '@/next/features/chat/types';
import type { RewindPlan } from '@/next/protocol/v2';

beforeAll(() => {
  pinLocale('ru');
  // The phone's layout: the question is a bottom sheet, opened by the tap itself.
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const PLAN: RewindPlan = {
  entryId: 't2',
  turns: 2,
  files: { at: 1, changes: [{ path: 'src/pay.ts', change: 'changed' }], more: 0 },
};

const actionsOf = (over: Partial<IRewindActions> = {}): IRewindActions => ({
  can: (turnId) => turnId !== 't1',
  busy: false,
  plan: () => Promise.resolve(PLAN),
  rewind: vi.fn(() => Promise.resolve()),
  stop: vi.fn(() => Promise.resolve()),
  undo: vi.fn(() => Promise.resolve()),
  rewound: () => Promise.resolve({ turns: [], entries: [], beforeTurnId: null }),
  ...over,
});

const prompt = (actions: IRewindActions | null, turnId = 't2') =>
  render(
    <RewindActions value={actions}>
      <RewindablePrompt turnId={turnId} text="Покажи адаптер">
        <p>bubble</p>
      </RewindablePrompt>
    </RewindActions>,
  );

describe('going on from an earlier message', () => {
  it('offers nothing without the chat’s rewinds, nor on the first message', () => {
    prompt(null);
    expect(screen.queryByText('Изменить и продолжить отсюда')).toBeNull();
    cleanup();
    prompt(actionsOf(), 't1');
    expect(screen.queryByText('Изменить и продолжить отсюда')).toBeNull();
  });

  it('edits the message in place and sends it with the conversation only', async () => {
    const actions = actionsOf();
    prompt(actions);
    fireEvent.click(screen.getByText('Изменить и продолжить отсюда'));
    const field = screen.getByLabelText('Новый текст сообщения');
    expect((field as HTMLTextAreaElement).value).toBe('Покажи адаптер');
    fireEvent.change(field, { target: { value: 'Покажи только маппинг' } });
    await act(async () => {});
    fireEvent.click(screen.getByText('Отправить'));
    await act(async () => {});
    expect(await screen.findByText('src/pay.ts')).toBeTruthy();
    fireEvent.click(screen.getByText('Только разговор'));
    await act(async () => {});
    expect(actions.rewind).toHaveBeenCalledWith('t2', 'Покажи только маппинг', false);
  });

  it('cannot take the files back without a snapshot', async () => {
    prompt(actionsOf({ plan: () => Promise.resolve({ ...PLAN, files: null }) }));
    fireEvent.click(screen.getByText('Изменить и продолжить отсюда'));
    await act(async () => {});
    fireEvent.click(screen.getByText('Отправить'));
    await act(async () => {});
    expect(await screen.findByText('Нет снимка файлов с того момента')).toBeTruthy();
    expect(screen.getByText('Разговор и файлы').closest('button')?.disabled).toBe(true);
  });

  it('asks to stop the agent first while it works', () => {
    const actions = actionsOf({ busy: true });
    prompt(actions);
    fireEvent.click(screen.getByText('Изменить и продолжить отсюда'));
    expect(screen.getByText('Отправить').closest('button')?.disabled).toBe(true);
    fireEvent.click(screen.getByText('Остановить'));
    expect(actions.stop).toHaveBeenCalled();
  });
});

describe('the turns a rewind left behind', () => {
  const item = (undoUntil: number | null): RewoundItem => ({
    kind: 'rewound',
    key: 'rewound:1',
    n: 0,
    at: 1,
    turns: 3,
    files: true,
    undoUntil,
  });

  it('reads «Откатено 3 хода» and can be taken back while its time lasts', () => {
    const actions = actionsOf();
    render(
      <RewindActions value={actions}>
        <RewoundRow item={item(Date.now() + 10_000)} />
      </RewindActions>,
    );
    expect(screen.getByText(/Откатено 3 хода/)).toBeTruthy();
    fireEvent.click(screen.getByText('Вернуть'));
    expect(actions.undo).toHaveBeenCalled();
  });

  it('has no «Вернуть» once it cannot be taken back', () => {
    render(
      <RewindActions value={actionsOf()}>
        <RewoundRow item={item(null)} />
      </RewindActions>,
    );
    expect(screen.queryByText('Вернуть')).toBeNull();
  });
});
