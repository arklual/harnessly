import { describe, expect, it } from 'vitest';

import { checkForm, emptyForm, problemsOf, splitList, toForm, toPolicy } from './network';

const PUB = btoa(String.fromCharCode(...new Uint8Array(32).fill(7)));
const BLOCK = ['-----BEGIN', 'PRIVATE', 'KEY-----'].join(' ');

describe('network policy form', () => {
  it('splits lists by lines, commas and repeats', () => {
    expect(splitList('A.corp\n a.corp, b.corp;;')).toEqual(['a.corp', 'b.corp']);
  });

  it('round-trips a policy and sends only what is set', () => {
    expect(toPolicy(emptyForm())).toEqual({});
    const p = {
      internalHosts: ['crm.bank.local'],
      proxy: 'http://p:3128',
      requireSigned: true,
      publishers: [{ name: 'Банк', key: PUB }],
    };
    expect(toPolicy(toForm(p))).toEqual(p);
  });

  it('names the field and the value of each mistake', () => {
    const f = {
      ...emptyForm(),
      internalHosts: 'https://crm.bank.local/x\nok.corp',
      caPem: BLOCK,
      proxy: 'ftp://x',
      noProxy: 'a b',
      publishers: [{ name: 'Банк', key: 'abc' }],
      requireSigned: true,
    };
    const ps = checkForm(f);
    expect(ps.map((p) => p.field)).toEqual([
      'internalHosts',
      'caPem',
      'proxy',
      'noProxy',
      'publishers',
      'requireSigned',
    ]);
    expect(ps[0]?.value).toBe('https://crm.bank.local/x');
  });

  it('accepts a good policy', () => {
    const f = {
      ...emptyForm(),
      internalHosts: 'crm.bank.local\n*.bank.local',
      caPem: '-----BEGIN CERTIFICATE-----\nAA\n-----END CERTIFICATE-----',
      proxy: 'proxy.bank.local:3128',
      noProxy: '*\n.bank.local\n10.0.0.0/8\n127.0.0.1',
      publishers: [{ name: 'Банк', key: PUB }],
      requireSigned: true,
    };
    expect(checkForm(f)).toEqual([]);
  });

  it("puts the hub's answer on fields", () => {
    expect(
      problemsOf({
        problems: [
          { field: 'proxy', message: 'плохо' },
          { field: 'x', message: 'y' },
        ],
      }),
    ).toEqual([{ field: 'proxy', text: 'плохо' }]);
    expect(problemsOf({})).toEqual([]);
  });
});
