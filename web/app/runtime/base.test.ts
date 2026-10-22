// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { apiUrl, homeSocketUrl, socketUrl } from '@/runtime/base';
import { currentMachineId, homeAppPath, scopedAppPath, setViewMachine } from '@/lib/app-path';

const open = (search: string) => window.history.replaceState(null, '', `/agents/workspace${search}`);

describe('calls of a page opened for one machine', () => {
  afterEach(() => {
    open('');
    document.head.innerHTML = '';
  });

  it('carry the machine of the page address', () => {
    document.head.innerHTML = '<meta name="api-base" content="/agents">';
    open('?runtime=1&machine=dev');
    expect(apiUrl('/api/workspace')).toBe('/agents/api/workspace?machine=dev');
    expect(apiUrl('/api/layout?workspace=ws-1#x')).toBe('/agents/api/layout?workspace=ws-1&machine=dev#x');
    expect(socketUrl('/api/terminal?session=s', { protocol: 'https:', host: 'ai.test' })).toBe(
      'wss://ai.test/agents/api/terminal?session=s&machine=dev',
    );
  });

  it('keep a machine the call names itself', () => {
    open('?machine=dev');
    expect(apiUrl('/api/status?machine=other')).toBe('/api/status?machine=other');
  });

  it('stay as they are on a page without one', () => {
    open('?runtime=1');
    expect(apiUrl('/api/workspace')).toBe('/api/workspace');
  });
});

describe('the machine of a call', () => {
  afterEach(() => {
    setViewMachine(undefined);
    open('');
  });

  const urls = () => ({
    api: apiUrl('/api/layout?workspace=ws-1'),
    socket: socketUrl('/api/timeline?session=s', { protocol: 'http:', host: 'h' }),
    status: homeSocketUrl('/api/status', { protocol: 'http:', host: 'h' }),
    app: scopedAppPath('/api/platform/prompt-queue'),
    home: homeAppPath('/api/platform/prompt-queue'),
    machine: currentMachineId(),
  });

  it('is the frame’s in a machine’s frame, on every path, whatever the shell draws', () => {
    const frame = {
      api: '/api/layout?workspace=ws-1&machine=dev',
      socket: 'ws://h/api/timeline?session=s&machine=dev',
      status: 'ws://h/api/status?machine=dev',
      app: '/api/platform/prompt-queue?machine=dev',
      home: '/api/platform/prompt-queue?machine=dev',
      machine: 'dev',
    };
    open('?machine=dev');
    expect(urls()).toEqual(frame);
    setViewMachine('other');
    expect(urls()).toEqual(frame);
  });

  it('is the drawn machine’s for the conversation’s calls without a frame, the home one for the shell’s own', () => {
    setViewMachine('m2');
    expect(urls()).toEqual({
      api: '/api/layout?workspace=ws-1&machine=m2',
      socket: 'ws://h/api/timeline?session=s&machine=m2',
      status: 'ws://h/api/status',
      app: '/api/platform/prompt-queue?machine=m2',
      home: '/api/platform/prompt-queue',
      machine: 'm2',
    });
  });

  it('is none on the home machine', () => {
    expect(urls()).toEqual({
      api: '/api/layout?workspace=ws-1',
      socket: 'ws://h/api/timeline?session=s',
      status: 'ws://h/api/status',
      app: '/api/platform/prompt-queue',
      home: '/api/platform/prompt-queue',
      machine: undefined,
    });
  });
});
