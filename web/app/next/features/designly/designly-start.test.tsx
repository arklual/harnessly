// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import DesignlyHome, { type IDesignStart } from '@/next/features/designly/designly-home';
import type { AttachmentRef, MachineId } from '@/next/protocol/v2';

vi.setConfig({ testTimeout: 30000 });

const uploads = () => ({
  attachments: [],
  ready: [{ id: 'a1' } as AttachmentRef],
  uploading: false,
  onFiles: vi.fn(),
  onRemove: vi.fn(),
  onRetry: vi.fn(),
  reset: vi.fn(),
});

const Home = ({
  brief = 'A promo of the new tariff',
  onCreate,
  initialKind,
}: {
  brief?: string;
  onCreate: (start: IDesignStart) => Promise<void>;
  initialKind?: IDesignStart['kind'];
}) => (
  <DesignlyHome
    cards={[]}
    error={null}
    now={0}
    onPin={vi.fn()}
    onOpen={vi.fn()}
    onRetry={vi.fn()}
    compose={{
      machine: 'm1' as MachineId,
      initialKind,
      systems: [{ id: 'acme', label: 'Acme', desc: 'Corporate' }],
      onCreate,
      brief,
      onBrief: vi.fn(),
      uploads: uploads(),
      picker: null,
      voice: { recording: null, onMic: vi.fn(), onCancel: vi.fn(), onFinish: vi.fn() },
    }}
  />
);

const tile = (name: string) => screen.getByText(name).closest('button') as HTMLElement;
const send = () => document.querySelector('.hx-dl__composer .hx-round[data-variant="accent"]') as HTMLElement;
const row = () =>
  [...(document.querySelector('.hx-dl__bar:not([hidden])') as HTMLElement).children]
    .filter((el) => el.tagName !== 'INPUT')
    .map((el) => el.className.split(' ')[0]);

beforeAll(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  window.matchMedia ??= ((query: string) =>
    ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }) as unknown as MediaQueryList) as typeof window.matchMedia;
});
afterEach(cleanup);

describe('the one field of Designly’s start', () => {
  it('stays on the screen when a tile changes the kind: the field and its row are the same', () => {
    render(<Home onCreate={vi.fn()} />);
    const field = document.querySelector('.hx-dl__input');
    const before = row();
    for (const name of ['Slides', 'Video', 'Site mockup']) {
      fireEvent.click(tile(name));
      expect(tile(name).getAttribute('aria-checked')).toBe('true');
      expect(document.querySelector('.hx-dl__input')).toBe(field);
      expect(row()).toEqual(before);
    }
  });

  it('offers no settings for a design system, which has none, and sends no design system for it', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<Home onCreate={onCreate} />);
    fireEvent.click(tile('Design system'));
    expect(document.querySelector('.hx-deck-chip')).toBeNull();
    fireEvent.click(send());
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1));
    expect(onCreate.mock.calls[0][0]).toMatchObject({
      kind: 'ds',
      designSystem: null,
      deck: null,
      video: null,
    });
  });

  it('sends a video with its settings, from the same field', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<Home onCreate={onCreate} />);
    fireEvent.click(tile('Video'));
    fireEvent.click(document.querySelector('.hx-deck-chip') as HTMLElement);
    const groups = document.querySelectorAll('.hx-deck-set [role="radiogroup"]');
    fireEvent.click(groups[1].querySelectorAll('button')[2]);
    fireEvent.click(groups[2].querySelectorAll('button')[2]);
    fireEvent.click(document.querySelector('.hx-deck-set [role="switch"]') as HTMLElement);
    fireEvent.click(send());
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1));
    expect(onCreate.mock.calls[0][0]).toEqual({
      brief: 'A promo of the new tariff',
      kind: 'video',
      designSystem: null,
      deck: null,
      video: { formats: ['16:9', '9:16'], seconds: 30, subs: false },
      attachmentIds: ['a1'],
    });
  });

  it('sends a presentation with its settings and a site with its design system alone', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined);
    render(<Home onCreate={onCreate} initialKind="slides" />);
    expect(tile('Slides').getAttribute('aria-checked')).toBe('true');
    fireEvent.click(send());
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(1));
    expect(onCreate.mock.calls[0][0]).toMatchObject({
      kind: 'slides',
      video: null,
      deck: { minutes: 0, slides: null, style: null },
    });
    fireEvent.click(tile('Site mockup'));
    fireEvent.click(document.querySelector('.hx-deck-chip') as HTMLElement);
    fireEvent.click(screen.getByText('Acme'));
    fireEvent.click(send());
    await waitFor(() => expect(onCreate).toHaveBeenCalledTimes(2));
    expect(onCreate.mock.calls[1][0]).toMatchObject({
      kind: 'site',
      designSystem: 'acme',
      deck: null,
      video: null,
    });
  });

  it('sends nothing for an empty brief and takes the person back to the field', () => {
    const onCreate = vi.fn();
    render(<Home brief="  " onCreate={onCreate} />);
    fireEvent.click(tile('Video'));
    fireEvent.click(send());
    expect(onCreate).not.toHaveBeenCalled();
    expect(document.activeElement).toBe(document.querySelector('.hx-dl__input'));
  });
});
