import { describe, expect, it } from 'vitest';
import { hostOf } from '@/next/features/settings/browser-agent-group';

describe('a site to block for the agent', () => {
  it('is a host, typed with or without the rest of an address', () => {
    expect(hostOf('Bank.Example.com')).toBe('bank.example.com');
    expect(hostOf('https://bank.example.com/login?x=1')).toBe('bank.example.com');
    expect(hostOf('localhost')).toBe('localhost');
    expect(hostOf('127.0.0.1:4400')).toBe('127.0.0.1');
    expect(hostOf('Bank.Example.com.')).toBe('bank.example.com');
    expect(hostOf('  ')).toBeNull();
    expect(hostOf('not a host')).toBeNull();
  });
});
