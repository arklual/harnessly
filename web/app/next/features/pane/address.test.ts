import { describe, expect, it } from 'vitest';
import { parseAddress } from '@/next/features/pane/address';

describe('parseAddress', () => {
  it('reads a bare port as this machine', () => {
    expect(parseAddress('4400')).toEqual({
      target: { kind: 'port', port: 4400 },
      path: '/',
      text: 'localhost:4400',
    });
    expect(parseAddress(':3000')?.target).toEqual({ kind: 'port', port: 3000 });
  });

  it('reads localhost with a path as a port', () => {
    expect(parseAddress('http://localhost:5173/admin?x=1')).toEqual({
      target: { kind: 'port', port: 5173 },
      path: '/admin?x=1',
      text: 'localhost:5173/admin?x=1',
    });
  });

  it('reads a host as a site over https unless http is typed', () => {
    expect(parseAddress('stand-2.preview.example.com/adminpanel')).toEqual({
      target: { kind: 'url', origin: 'https://stand-2.preview.example.com' },
      path: '/adminpanel',
      text: 'stand-2.preview.example.com/adminpanel',
    });
    expect(parseAddress('http://10.0.0.5:8080')?.target).toEqual({
      kind: 'url',
      origin: 'http://10.0.0.5:8080',
    });
  });

  it('is not an address for words and a port-less localhost', () => {
    expect(parseAddress('терминал')).toBeNull();
    expect(parseAddress('localhost')).toBeNull();
    expect(parseAddress('99999')).toBeNull();
    expect(parseAddress('')).toBeNull();
  });
});
