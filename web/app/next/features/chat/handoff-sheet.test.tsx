// @vitest-environment jsdom
import { createRef, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import HandoffSheet, { SHEET_MODE, type TSheetMode } from '@/next/features/chat/handoff-sheet';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const ARTIFACT_ID = '0123456789abcdef0123456789abcdef';

describe('handoff sheet', () => {
  it('offers full history as metadata, never as editable transcript text', async () => {
    const onSend = vi.fn().mockResolvedValue(undefined);
    const onDraft = vi.fn(async (mode: TSheetMode) => ({
      ok: true as const,
      draft:
        mode === SHEET_MODE.history
          ? { mode: 'history' as const, tokens: 0, turns: 0, artifact: { id: ARTIFACT_ID, bytes: 292552704 } }
          : { mode, text: 'editable context', tokens: 4, turns: 1 },
    }));
    inRoot(
      <HandoffSheet
        open
        onClose={() => undefined}
        title="Continue"
        lead="Choose context"
        modes={[SHEET_MODE.excerpt, SHEET_MODE.note, SHEET_MODE.history]}
        onDraft={onDraft}
        onSend={onSend}
        sendLabel="Continue"
      />,
    );

    expect(await screen.findByRole('textbox', { name: 'The text the chat starts with' })).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: 'Full history' }));
    expect(await screen.findByText(/292,552,704 bytes/)).toBeTruthy();
    expect(screen.queryByRole('textbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => expect(onSend).toHaveBeenCalledWith('history', '', ARTIFACT_ID));
  });

  it('keeps one history draft while switching modes', async () => {
    const onDraft = vi.fn(async (mode: TSheetMode) => ({
      ok: true as const,
      draft:
        mode === SHEET_MODE.history
          ? { mode: 'history' as const, tokens: 0, turns: 0, artifact: { id: ARTIFACT_ID, bytes: 42 } }
          : { mode, text: 'editable context', tokens: 4, turns: 1 },
    }));
    inRoot(
      <HandoffSheet
        open
        onClose={() => undefined}
        title="Continue"
        lead="Choose context"
        modes={[SHEET_MODE.excerpt, SHEET_MODE.note, SHEET_MODE.history]}
        onDraft={onDraft}
        onSend={vi.fn().mockResolvedValue(undefined)}
        sendLabel="Continue"
      />,
    );
    await screen.findByRole('textbox');
    fireEvent.click(screen.getByRole('radio', { name: 'Full history' }));
    await screen.findByText(/42 bytes/);
    fireEvent.click(screen.getByRole('radio', { name: 'Excerpt' }));
    fireEvent.click(screen.getByRole('radio', { name: 'Full history' }));
    await screen.findByText(/42 bytes/);
    expect(onDraft.mock.calls.filter(([mode]) => mode === SHEET_MODE.history)).toHaveLength(1);
  });
});
