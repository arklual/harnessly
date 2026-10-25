// @vitest-environment jsdom
// Claude Code wraps a burst it takes for a paste (a dictated text the typist types in one go) in pasted_content; the
// bubble of a message the person sent from this page stays a bubble, only a paste typed into the agent is a pasted block.
import { cleanup, render } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import UserMessage from '@/next/features/chat/user-message';

beforeAll(() => pinLocale('ru'));
afterEach(cleanup);

const WRAPPED = '<pasted_content id="7">\nБратан, это надиктованный текст\n</pasted_content id="7">';
const NO_ACTION = () => Promise.resolve();

const draw = (own: boolean) =>
  render(
    <UserMessage
      text={WRAPPED}
      attachments={[]}
      outgoing={null}
      own={own}
      offline={false}
      onRetry={NO_ACTION}
      onDelete={NO_ACTION}
    />,
  );

describe('UserMessage pasted wrapper', () => {
  it("is a plain bubble for the page's own message", () => {
    const { container } = draw(true);
    expect(container.querySelector('.hx-paste')).toBeNull();
    expect(container.querySelector('.hx-bubble p')?.textContent).toBe('Братан, это надиктованный текст');
  });

  it("is a pasted block for a paste made in the agent's own terminal", () => {
    const { container } = draw(false);
    expect(container.querySelector('.hx-paste')).not.toBeNull();
  });
});
