import { describe, expect, it } from 'vitest';
import { isLoginLost } from '@/next/features/chat/login-card';

describe('isLoginLost', () => {
  it('knows the agents’ words for a sign-in that ran out', () => {
    for (const text of [
      'Login expired · Please run /login',
      'Invalid API key · Please run /login',
      'OAuth token has expired. Please obtain a new token or refresh your existing token.',
      'Not logged in · Please run /login',
      'Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.',
    ])
      expect(isLoginLost(text)).toBe(true);
  });

  it('leaves other failures and the early warning alone', () => {
    for (const text of ['API Error: 529 Overloaded', 'Prompt is too long', null, ''])
      expect(isLoginLost(text)).toBe(false);
  });
});
