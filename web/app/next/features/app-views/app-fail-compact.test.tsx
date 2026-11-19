// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import AppFail from '@/next/features/app-views/app-fail';
import { FAIL_REASON } from '@/next/features/app-views/frame-health';

const app = { id: 'local.widget-render-progress', name: 'Рендер ролика', views: [] } as never;

describe('a widget that did not start (HRN-1665)', () => {
  it('shows one icon and one button, no prose and no journal; the reason is a tooltip', () => {
    const retry = vi.fn();
    const { container } = render(
      <AppFail
        machine={'m' as never}
        app={app}
        failure={{ reason: FAIL_REASON.server, detail: 'Машина не отвечает' } as never}
        logKey="k"
        conversation={null}
        onRetry={retry}
        compact
      />,
    );
    expect(container.textContent).toMatch(/^(Обновить|Refresh)$/);
    expect(screen.queryByText(/журнал|journal|log/i)).toBeNull();
    expect(screen.queryByText(/не запустилось|did not start/)).toBeNull();
    expect(container.querySelector('[data-compact]')?.getAttribute('title')).toMatch(
      /Сервер приложения не отвечает|not responding|does not answer/i,
    );
    fireEvent.click(screen.getByRole('button', { name: /Обновить|Refresh/ }));
    expect(retry).toHaveBeenCalledOnce();
  });
});
