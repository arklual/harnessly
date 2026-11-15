// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppStage, {
  appStageDesign,
  SPACES_STAGE,
  type IAppStageProps,
} from '@/next/features/app-main/app-stage';
import type { ISiteStage } from '@/next/features/designly/site-view';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const stage = (over: Partial<ISiteStage> = {}): ISiteStage => ({
  conv: null,
  mode: 'chat',
  onMode: vi.fn(),
  min: false,
  run: 'idle',
  float: null,
  dock: 'left',
  chatW: 372,
  chat: <div data-testid="chat" />,
  island: <div data-testid="island" />,
  overlay: null,
  pins: null,
  pick: false,
  onPicked: vi.fn(),
  onPickEnd: vi.fn(),
  onBridge: vi.fn(),
  styles: [],
  onStyled: vi.fn(),
  anchors: [],
  picking: null,
  onPick: vi.fn(),
  sview: false,
  onSview: vi.fn(),
  onAttach: vi.fn(),
  ...over,
});

const props = (over: Partial<IAppStageProps> = {}): IAppStageProps => ({
  title: 'Главная',
  stage: stage(),
  frame: <div data-testid="frame" />,
  inspector: null,
  menu: [],
  onShare: vi.fn(),
  toolPane: null,
  onOpenDrawer: vi.fn(),
  ...over,
});

describe('an app in Sinopia’s stage', () => {
  it('keys the home and each object apart; the spaces share one stage (HRN-1469)', () => {
    expect(appStageDesign('acme.cashback', null)).toBe('app:acme.cashback');
    expect(appStageDesign('acme.cashback', 'plan-x1')).toBe('app:acme.cashback/plan-x1');
    expect(appStageDesign('harnessly.docs', null)).toBe(SPACES_STAGE);
    expect(appStageDesign('harnessly.docs', 'plan-x1')).toBe(SPACES_STAGE);
    expect(appStageDesign('harnessly.boards', 'b1')).toBe(SPACES_STAGE);
  });

  it('stands in the design’s section, with the chat, the view and the field', () => {
    const { container } = render(<AppStage {...props()} />);
    const section = container.querySelector('section.hx-site');
    expect(section?.getAttribute('data-mode')).toBe('chat');
    expect(section?.getAttribute('data-dock')).toBe('left');
    expect(screen.getByTestId('chat')).toBeTruthy();
    expect(screen.getByTestId('frame')).toBeTruthy();
    expect(screen.getByTestId('island')).toBeTruthy();
    expect((section as HTMLElement | null)?.style.getPropertyValue('--site-chat')).toBe('372px');
  });

  it('names what is open with its place, never the app, and goes back from it', () => {
    const onBack = vi.fn();
    render(<AppStage {...props({ title: 'plan-x1', crumb: 'acme/runtime !12', onBack })} />);
    expect(document.querySelector('.hx-appmain__title')?.textContent).toBe('plan-x1');
    expect(document.querySelector('.hx-appmain__crumb')?.textContent).toBe('acme/runtime !12');
    fireEvent.click(screen.getByRole('button', { name: /^(Back|Назад)$/ }));
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it('folds a screen opened from a chat with its own «Collapse», not a back arrow', () => {
    const onCollapse = vi.fn();
    render(<AppStage {...props({ onBack: vi.fn(), onCollapse })} />);
    expect(screen.queryByRole('button', { name: /^(Back|Назад)$/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /^(Collapse|Свернуть)$/ }));
    expect(onCollapse).toHaveBeenCalledTimes(1);
  });

  it('offers the chat’s places when the chat is folded, and the right pane', () => {
    const onMode = vi.fn();
    const onToggle = vi.fn();
    const { container } = render(
      <AppStage
        {...props({ stage: stage({ mode: 'island', onMode }), toolPane: { open: false, onToggle } })}
      />,
    );
    expect(container.querySelector('.hx-placement')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Инструменты|Tools/ }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
