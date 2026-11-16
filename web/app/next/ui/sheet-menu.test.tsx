// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MenuItem, MenuSeparator, SheetMenu } from '@/next/ui/sheet-menu';

const draw = (props: { onPick?: () => void; onOpenChange?: (open: boolean) => void } = {}) =>
  render(
    <SheetMenu
      trigger={<button type="button">More</button>}
      label="Chat menu"
      className="hx-cmenu hx-sheet-phone"
      onOpenChange={props.onOpenChange}
    >
      <MenuItem className="hx-cmenu__item" onClick={props.onPick}>
        Rename
      </MenuItem>
      <MenuSeparator className="hx-cmenu__sep" />
      <MenuItem className="hx-cmenu__item" closeOnClick={false}>
        Move
      </MenuItem>
      <MenuItem className="hx-cmenu__item" data-danger="true">
        Delete
      </MenuItem>
    </SheetMenu>,
  );

describe('the phone menu is a sheet of our own (HRN-1491)', () => {
  afterEach(cleanup);

  it('opens from its trigger as a menu with its items', () => {
    const onOpenChange = vi.fn();
    draw({ onOpenChange });
    const trigger = screen.getByRole('button', { name: 'More' });
    expect(trigger.getAttribute('aria-haspopup')).toBe('menu');
    fireEvent.click(trigger);
    expect(onOpenChange).toHaveBeenCalledWith(true);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');
    const menu = screen.getByRole('menu', { name: 'Chat menu' });
    expect(menu.querySelectorAll('[role="menuitem"]')).toHaveLength(3);
    expect(menu.querySelector('[role="separator"]')).not.toBeNull();
  });

  it('the arrows go round the items, Home and End to the ends', () => {
    draw();
    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    const items = screen.getAllByRole('menuitem');
    fireEvent.keyDown(screen.getByRole('menu'), { key: 'ArrowDown' });
    expect(document.activeElement).toBe(items[0]);
    fireEvent.keyDown(items[0], { key: 'ArrowUp' });
    expect(document.activeElement).toBe(items[2]);
    fireEvent.keyDown(items[2], { key: 'ArrowDown' });
    expect(document.activeElement).toBe(items[0]);
    fireEvent.keyDown(items[0], { key: 'End' });
    expect(document.activeElement).toBe(items[2]);
    fireEvent.keyDown(items[2], { key: 'Home' });
    expect(document.activeElement).toBe(items[0]);
  });

  it('an item does its thing and closes the menu; one that leads further keeps it', () => {
    const onPick = vi.fn();
    const onOpenChange = vi.fn();
    draw({ onPick, onOpenChange });
    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Move' }));
    expect(onOpenChange).not.toHaveBeenCalledWith(false);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }));
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onOpenChange).toHaveBeenLastCalledWith(false);
    expect(screen.getByRole('button', { name: 'More' }).getAttribute('aria-expanded')).toBe('false');
  });
});
