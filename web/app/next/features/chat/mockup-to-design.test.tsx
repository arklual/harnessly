// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const mockupToDesign = vi.fn();
vi.mock('@/next/features/designly/api', () => ({
  mockupToDesign: (...a: unknown[]) => mockupToDesign(...a),
}));

import MockupToDesign, {
  AnswerText,
  mockupTitle,
  type IMockupHost,
} from '@/next/features/chat/mockup-to-design';
import { clearDraft, draftRefs } from '@/next/stores/drafts';
import type { ConvId, DesignlyMockup, MachineId } from '@/next/protocol/v2';

const conv = 'tab-a1b2c3' as ConvId;
const html = '<div class="card"><h2>Декабрь · Mass/Pro</h2></div>';
const made: DesignlyMockup = {
  project: 'dekabr-mass-pro',
  projectTitle: 'Декабрь · Mass/Pro',
  page: '01JD3Q4M5N6P7Q8R9S0T1V2W3X',
  file: 'Dekabr-Mass-Pro.dc.html',
  title: 'Декабрь · Mass/Pro',
  newProject: true,
  existing: false,
  opened: true,
};

const deferred = <T,>() => {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

const host = (open = vi.fn()): IMockupHost => ({ machine: 'm1' as MachineId, conv, open });

beforeEach(() => {
  localStorage.clear();
  clearDraft(conv);
  mockupToDesign.mockReset();
});
afterEach(cleanup);

describe('«Continue in Sinopia Design» on an html card', () => {
  it('shows progress, then opens the board and names it on the chat field', async () => {
    const answer = deferred<DesignlyMockup>();
    mockupToDesign.mockReturnValue(answer.promise);
    const open = vi.fn();
    render(<MockupToDesign html={html} host={host(open)} />);
    fireEvent.click(screen.getByRole('button', { name: 'Continue in Sinopia Design' }));
    const busy = screen.getByRole('button', { name: 'Making the board…' });
    expect(busy).toHaveProperty('disabled', true);
    expect(busy.getAttribute('data-busy')).toBe('true');
    expect(mockupToDesign).toHaveBeenCalledWith('m1', {
      conv,
      html,
      title: 'Декабрь · Mass/Pro',
      project: '',
    });
    await act(async () => answer.resolve(made));
    expect(await screen.findByRole('button', { name: 'Open in Sinopia Design' })).toBeTruthy();
    expect(draftRefs(conv)).toEqual([
      { kind: 'design', id: made.project, title: made.projectTitle, target: made.file },
    ]);
    // The machine asked the chat's pane itself: the page does not open it a second time.
    expect(open).not.toHaveBeenCalled();
  });

  it('a second press opens the same board and puts no second chip on the field', async () => {
    mockupToDesign
      .mockResolvedValueOnce(made)
      .mockResolvedValueOnce({ ...made, existing: true, newProject: false, opened: false });
    const open = vi.fn();
    render(<MockupToDesign html={html} host={host(open)} />);
    fireEvent.click(screen.getByRole('button', { name: 'Continue in Sinopia Design' }));
    await screen.findByRole('button', { name: 'Open in Sinopia Design' });
    fireEvent.click(screen.getByRole('button', { name: 'Open in Sinopia Design' }));
    await waitFor(() => expect(open).toHaveBeenCalledWith('m1', made.project, conv));
    expect(draftRefs(conv)).toHaveLength(1);
  });

  it('remembers the board after a reload when its storage key resolves after the click', async () => {
    const hash = deferred<ArrayBuffer>();
    const digest = vi.spyOn(crypto.subtle, 'digest').mockReturnValue(hash.promise);
    try {
      mockupToDesign.mockResolvedValue(made);
      const first = render(<MockupToDesign html={html} host={host()} />);
      fireEvent.click(screen.getByRole('button', { name: 'Continue in Sinopia Design' }));
      await act(async () => hash.resolve(new ArrayBuffer(32)));
      await screen.findByRole('button', { name: 'Open in Sinopia Design' });
      first.unmount();
      render(<MockupToDesign html={html} host={host()} />);
      expect(await screen.findByRole('button', { name: 'Open in Sinopia Design' })).toBeTruthy();
    } finally {
      digest.mockRestore();
    }
  });

  it('a failure is a quiet line and «Retry»', async () => {
    mockupToDesign.mockRejectedValueOnce(new Error('Нет доступа к проекту')).mockResolvedValueOnce(made);
    render(<MockupToDesign html={html} host={host()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Continue in Sinopia Design' }));
    expect(await screen.findByRole('status')).toHaveProperty('textContent', 'Нет доступа к проекту');
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('button', { name: 'Open in Sinopia Design' })).toBeTruthy();
    expect(screen.queryByRole('status')).toBeNull();
  });
});

describe('the mockup’s title', () => {
  it('is its heading, else the answer’s first line', () => {
    expect(mockupTitle('<title> Шаги </title><h1>x</h1>', '')).toBe('Шаги');
    expect(mockupTitle('<div><h2>Декабрь</h2></div>', 'Ответ')).toBe('Декабрь');
    expect(
      mockupTitle('<div>без заголовка</div>', '\n**Упростил:** на каждом шаге одно действие.\n```html'),
    ).toBe('Упростил: на каждом шаге одно действие');
  });

  it('comes from the answer the card is in', () => {
    mockupToDesign.mockReturnValue(new Promise(() => {}));
    render(
      <AnswerText value="Карточка подбора">
        <MockupToDesign html="<p>x</p>" host={host()} />
      </AnswerText>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Continue in Sinopia Design' }));
    expect(mockupToDesign.mock.calls[0][1].title).toBe('Карточка подбора');
  });
});
