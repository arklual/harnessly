import { describe, expect, it } from 'vitest';
import { permTitleKey } from '@/next/features/questions/gate-title';

describe('permTitleKey', () => {
  it('titles a permission by what the tool does, not always as a command', () => {
    expect(permTitleKey('Bash')).toBe('next.gate.permTitleCommand');
    expect(permTitleKey('Read')).toBe('next.gate.permTitleRead');
    expect(permTitleKey('Edit')).toBe('next.gate.permTitleEdit');
    expect(permTitleKey('Write')).toBe('next.gate.permTitleWrite');
    expect(permTitleKey('WebFetch')).toBe('next.gate.permTitleWeb');
    expect(permTitleKey('mcp__github__create_issue')).toBe('next.gate.permTitleMcp');
    expect(permTitleKey('exec')).toBe('next.gate.permTitleCommand');
    expect(permTitleKey('apply-patch')).toBe('next.gate.permTitlePatch');
  });

  it('asks about «the action» for a tool nobody knows', () => {
    expect(permTitleKey('Brand_new')).toBe('next.gate.permTitle');
    expect(permTitleKey(null)).toBe('next.gate.permTitle');
    expect(permTitleKey(null, 'ls -la')).toBe('next.gate.permTitleCommand');
  });
});
