// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { TenantRef } from '../../../org/types';
import { NewTeamSheet } from './extra';

const tenants = [
  { id: 'a', slug: 'a', name: 'Alpha' },
  { id: 'b', slug: 'b', name: 'Beta' },
] as unknown as TenantRef[];

afterEach(cleanup);

// GitHub #127: the button looked active while the form could not be sent, and said nothing.
describe('New Team button', () => {
  it('looks disabled and names what is missing', () => {
    const apply = vi.fn(async () => true);
    render(
      <NewTeamSheet open onClose={() => {}} apply={apply} problem={null} taken={[]} tenants={tenants} />,
    );
    const button = screen.getByRole('button', { name: /Создать/ });
    expect(button.getAttribute('aria-disabled')).toBe('true');
    expect(screen.getByText('Выберите тенант')).toBeTruthy();
    fireEvent.click(button);
    expect(apply).not.toHaveBeenCalled();
  });

  it('asks for the name once the tenant is the only one', () => {
    render(
      <NewTeamSheet
        open
        onClose={() => {}}
        apply={async () => true}
        problem={null}
        taken={[]}
        tenants={tenants.slice(0, 1)}
      />,
    );
    expect(screen.getByText('Введите название команды')).toBeTruthy();
  });
});
