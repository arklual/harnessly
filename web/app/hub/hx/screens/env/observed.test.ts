import { describe, expect, it } from 'vitest';

import { toolLabel } from './observed';

describe('toolLabel', () => {
  it('names a plugin skill without its plugin', () => {
    expect(toolLabel({ kind: 'skill', name: 'tools:release' })).toEqual({ title: 'release', from: 'tools' });
    expect(toolLabel({ kind: 'skill', name: 'review:review' })).toEqual({ title: 'review', from: '' });
    expect(toolLabel({ kind: 'skill', name: 'review' })).toEqual({ title: 'review', from: '' });
    expect(toolLabel({ kind: 'skill', name: 'odd:' })).toEqual({ title: 'odd:', from: '' });
  });

  it('names a plugin or connector MCP by its short name', () => {
    expect(toolLabel({ kind: 'mcp', name: 'plugin_tools_grafana' })).toEqual({
      title: 'grafana',
      from: 'tools',
    });
    expect(toolLabel({ kind: 'mcp', name: 'plugin_tools_error_tracker' })).toEqual({
      title: 'error tracker',
      from: 'tools',
    });
    expect(toolLabel({ kind: 'mcp', name: 'claude_ai_Claude_Docs' })).toEqual({
      title: 'Claude Docs',
      from: 'claude.ai',
    });
    expect(toolLabel({ kind: 'mcp', name: 'gitlab' })).toEqual({ title: 'gitlab', from: '' });
  });
});
