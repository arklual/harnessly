import { describe, expect, it } from 'vitest';
import { openFailure } from '@/next/core/net/preview';
import { RuntimeError } from '@/next/core/net/runtime-json';

describe('openFailure', () => {
  it('puts a refusal code in words and keeps what the runtime answered', () => {
    const said = new RuntimeError('invalid slot', { error: 'invalid slot' });
    const out = openFailure(said) as RuntimeError;
    expect(out).toBeInstanceOf(RuntimeError);
    expect(out.message).not.toBe('invalid slot');
    expect(out.message).not.toMatch(/slot/);
    expect(out.body).toEqual({ error: 'invalid slot' });
  });

  it('leaves a sentence and a foreign error as they are', () => {
    const plain = new RuntimeError('Машина не отвечает');
    expect(openFailure(plain)).toBe(plain);
    const other = new Error('boom');
    expect(openFailure(other)).toBe(other);
  });
});
