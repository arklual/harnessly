// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import type { ICodexLogin } from '@/next/features/settings/codex-logins-api';
import CodexLoginsGroup, { CodexLoginCard } from '@/next/features/settings/codex-logins-group';
import { pinLocale } from '@/next/i18n';

beforeAll(() => {
  pinLocale('ru');
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const second: ICodexLogin = {
  id: 'ab12',
  name: 'Рабочий',
  profile: 'cx-ab12',
  account: 'work@example.com',
  main: false,
};
const empty: ICodexLogin = { id: 'cd34', name: 'Личный', profile: 'cx-cd34', account: '', main: false };

describe('CodexLoginsGroup', () => {
  it('draws nothing until the list is read', () => {
    const { container } = render(
      <CodexLoginsGroup logins={null} full={false} onAdd={async () => null} renderLogin={() => null} />,
    );
    expect(container.innerHTML).toBe('');
  });

  it('adds a sign-in by its name and keeps the words of a refusal', async () => {
    const onAdd = vi
      .fn<(name: string) => Promise<string | null>>()
      .mockResolvedValueOnce('Такое название уже есть');
    render(<CodexLoginsGroup logins={[]} full={false} onAdd={onAdd} renderLogin={() => null} />);
    // The name opens in place under «Добавить вход».
    expect(screen.queryByLabelText('Название входа')).toBeNull();
    fireEvent.click(screen.getByText('Добавить вход'));
    const field = screen.getByLabelText('Название входа') as HTMLInputElement;
    fireEvent.change(field, { target: { value: '  Рабочий ' } });
    await act(async () => {
      fireEvent.click(screen.getByText('Добавить'));
    });
    expect(onAdd).toHaveBeenCalledWith('Рабочий');
    expect(screen.getByText('Такое название уже есть')).toBeTruthy();
    expect(field.value).toBe('  Рабочий ');
    onAdd.mockResolvedValueOnce(null);
    await act(async () => {
      fireEvent.click(screen.getByText('Добавить'));
    });
    expect(field.value).toBe('');
  });

  it('offers no more sign-ins when the machine keeps as many as it can', () => {
    render(
      <CodexLoginsGroup
        logins={[second]}
        full
        onAdd={async () => null}
        renderLogin={(l) => <i key={l.id} />}
      />,
    );
    expect(screen.queryByText('Добавить вход')).toBeNull();
  });
});

describe('CodexLoginCard', () => {
  it('says whose plan a sign-in holds, or that it is not signed in', () => {
    render(
      <>
        <CodexLoginCard
          login={second}
          onRename={async () => null}
          onRemove={async () => null}
          onMain={async () => null}
        >
          <i />
        </CodexLoginCard>
        <CodexLoginCard
          login={empty}
          onRename={async () => null}
          onRemove={async () => null}
          onMain={async () => null}
        >
          <i />
        </CodexLoginCard>
      </>,
    );
    expect(screen.getByText('work@example.com')).toBeTruthy();
    expect(screen.getByText('Вход не выполнен')).toBeTruthy();
  });

  it('removes only after a second press', async () => {
    const onRemove = vi.fn(async () => null);
    render(
      <CodexLoginCard
        login={second}
        onRename={async () => null}
        onRemove={onRemove}
        onMain={async () => null}
      >
        <i />
      </CodexLoginCard>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Вход «Рабочий»: действия' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Удалить' }));
    expect(onRemove).not.toHaveBeenCalled();
    expect(screen.getByText('Точно удалить? Вместе с историей чатов этого входа')).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    });
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it('renames with the typed name', async () => {
    const onRename = vi.fn(async () => null);
    render(
      <CodexLoginCard
        login={second}
        onRename={onRename}
        onRemove={async () => null}
        onMain={async () => null}
      >
        <i />
      </CodexLoginCard>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Вход «Рабочий»: действия' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Переименовать' }));
    fireEvent.change(screen.getByLabelText('Название входа'), { target: { value: 'Команда' } });
    await act(async () => {
      fireEvent.click(screen.getByText('Сохранить'));
    });
    expect(onRename).toHaveBeenCalledWith('Команда');
  });

  it('makes a signed-in sign-in the main one and gives that back', async () => {
    const onMain = vi.fn(async () => null);
    const { rerender } = render(
      <CodexLoginCard login={second} onRename={async () => null} onRemove={async () => null} onMain={onMain}>
        <i />
      </CodexLoginCard>,
    );
    expect(screen.queryByText(/основной/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Вход «Рабочий»: действия' }));
    await act(async () => {
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Сделать основным' }));
    });
    expect(onMain).toHaveBeenLastCalledWith(true);
    rerender(
      <CodexLoginCard
        login={{ ...second, main: true }}
        onRename={async () => null}
        onRemove={async () => null}
        onMain={onMain}
      >
        <i />
      </CodexLoginCard>,
    );
    expect(screen.getByText('work@example.com · основной')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Вход «Рабочий»: действия' }));
    await act(async () => {
      fireEvent.click(await screen.findByRole('menuitem', { name: 'Снять основной' }));
    });
    expect(onMain).toHaveBeenLastCalledWith(false);
  });

  it('offers no main mark to a sign-in that is not signed in, and «Войти» opens its sign-in', async () => {
    render(
      <CodexLoginCard
        login={empty}
        onRename={async () => null}
        onRemove={async () => null}
        onMain={async () => null}
      >
        <i data-testid="panel" />
      </CodexLoginCard>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Вход «Личный»: действия' }));
    await screen.findByRole('menuitem', { name: 'Переименовать' });
    expect(screen.queryByRole('menuitem', { name: 'Сделать основным' })).toBeNull();
    expect(screen.queryByTestId('panel')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Войти' }));
    expect(screen.getByTestId('panel')).toBeTruthy();
  });
});
