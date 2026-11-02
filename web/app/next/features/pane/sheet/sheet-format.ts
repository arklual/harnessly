// What a spreadsheet's cell shows: its stored value through the file's number format. The commonest formats exactly; an
// unusual one falls to the plain number, never to a wrong date or a lost digit.

const BUILTIN: Readonly<Record<number, string>> = {
  1: '0',
  2: '0.00',
  3: '#,##0',
  4: '#,##0.00',
  9: '0%',
  10: '0.00%',
  11: '0.00E+00',
  14: 'dd.mm.yyyy',
  15: 'd-mmm-yy',
  16: 'd-mmm',
  17: 'mmm-yy',
  18: 'h:mm AM/PM',
  19: 'h:mm:ss AM/PM',
  20: 'h:mm',
  21: 'h:mm:ss',
  22: 'dd.mm.yyyy h:mm',
  37: '#,##0',
  38: '#,##0',
  39: '#,##0.00',
  40: '#,##0.00',
  45: 'mm:ss',
  46: '[h]:mm:ss',
  47: 'mm:ss.0',
  48: '0.0E+00',
  49: '@',
};

export const formatCodeOf = (id: number, custom: ReadonlyMap<number, string>): string =>
  custom.get(id) ?? BUILTIN[id] ?? 'General';

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];

/** A format's first section without its literals and colours: what decides the kind of value. */
const sectionOf = (code: string): string =>
  (code.split(';')[0] ?? '')
    .replace(/"[^"]*"/g, '')
    .replace(/\\./g, '')
    .replace(/\[(?:[a-zA-Z$][^\]]*)\]/g, '')
    .replace(/_./g, '')
    .replace(/\*./g, '');

export const isDateFormat = (code: string): boolean => {
  const section = sectionOf(code).toLowerCase();
  return /[dmyhs]/.test(section) && !/[0#]\.?[0#]*[e%]?$/.test(section.replace(/[dmyhs:./\- ]/g, ''));
};

const pad = (value: number, width = 2) => String(value).padStart(width, '0');

const formatDate = (serial: number, code: string, date1904: boolean): string => {
  const days = date1904 ? serial + 1462 : serial;
  const ms = Math.round((days - 25569) * 86400000);
  const at = new Date(ms);
  if (Number.isNaN(at.getTime())) return String(serial);
  const lower = code.toLowerCase();
  const ampm = /am\/pm|a\/p/.test(lower);
  const hours = at.getUTCHours();
  const parts = sectionOf(code).toLowerCase();
  // «m» after an hour or before a second is minutes.
  const tokens = parts.match(/y+|m+|d+|h+|s+|[^ymdhs]+/g) ?? [];
  let out = '';
  tokens.forEach((token, index) => {
    const previous =
      tokens
        .slice(0, index)
        .reverse()
        .find((t) => /^[ymdhs]/.test(t)) ?? '';
    const next = tokens.slice(index + 1).find((t) => /^[ymdhs]/.test(t)) ?? '';
    if (token.startsWith('y')) out += token.length > 2 ? at.getUTCFullYear() : pad(at.getUTCFullYear() % 100);
    else if (token.startsWith('d')) out += token.length > 1 ? pad(at.getUTCDate()) : at.getUTCDate();
    else if (token.startsWith('h')) {
      const shown = ampm ? hours % 12 || 12 : hours;
      out += token.length > 1 ? pad(shown) : shown;
    } else if (token.startsWith('s')) out += pad(at.getUTCSeconds());
    else if (token.startsWith('m')) {
      if (previous.startsWith('h') || next.startsWith('s')) out += pad(at.getUTCMinutes());
      else if (token.length >= 3) out += MONTHS[at.getUTCMonth()];
      else out += token.length > 1 ? pad(at.getUTCMonth() + 1) : at.getUTCMonth() + 1;
    } else out += token.replace(/am\/pm|a\/p/g, '');
  });
  if (ampm) out = `${out.trim()} ${hours < 12 ? 'AM' : 'PM'}`;
  return out.trim();
};

const groupThousands = (digits: string): string => digits.replace(/\B(?=(\d{3})+(?!\d))/g, ' ');

/** A number by the format's zeros, thousands, percent and exponent. */
const formatNumber = (value: number, code: string): string => {
  const section = sectionOf(code);
  if (/e\+/i.test(section)) {
    const places = (/\.(0+)/.exec(section)?.[1] ?? '').length;
    return value
      .toExponential(places)
      .toUpperCase()
      .replace('E+', 'E+')
      .replace(/E(-?)(\d)$/, 'E$10$2');
  }
  if (!/[0#]/.test(section)) return plainNumber(value);
  const percent = section.includes('%');
  const scaled = percent ? value * 100 : value;
  const places = (/\.([0#]+)/.exec(section)?.[1] ?? '').length;
  const fixed = Math.abs(scaled).toFixed(places);
  const [whole = '0', fraction] = fixed.split('.');
  const sign = scaled < 0 ? '-' : '';
  const body = section.includes(',') ? groupThousands(whole) : whole;
  return `${sign}${body}${fraction ? `,${fraction}` : ''}${percent ? '%' : ''}`;
};

/** A number with no format: as many digits as it has, up to 11 significant, as a spreadsheet does. */
export const plainNumber = (value: number): string => String(Number(value.toPrecision(11))).replace('.', ',');

export const formatValue = (value: number, code: string, date1904: boolean): string => {
  if (code === 'General' || code === '@') return plainNumber(value);
  if (isDateFormat(code)) return formatDate(value, code, date1904);
  return formatNumber(value, code);
};
