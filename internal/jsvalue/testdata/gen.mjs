// Writes cases.json: each input JSON text with what JSON.parse + JSON.stringify make of it, compact and with 2 spaces.
// Run with node from this folder: node gen.mjs
import fs from 'node:fs';

const inputs = [
  '{"b":1,"a":2,"c":{"z":[1,2,{"y":null}],"x":true}}',
  '{"a":1,"b":2,"a":3}',
  '{"2":"two","b":"b","1":"one","01":"zero-one","a":"a","4294967295":"big","4294967294":"max"}',
  '[]', '{}', '[[],{},[{}]]', 'null', 'true', '"str"', '0', '-0', '1e21', '1e-7', '123456789012345678901', '0.1', '1.5e300', '1e400',
  '[0.000001, 1e-6, 5e-324, 1.7976931348623157e308, 100, 1e20, 12.5, -3.25e-10]',
  '"esc \\" \\\\ \\/ \\b \\f \\n \\r \\t \\u0001 \\u001f \\u007f \\u2028 \\u2029 é 😀 \\ud83d\\ude00"',
  '{"nested":{"deep":{"deeper":[1,[2,[3,[]]]]}},"after":"x"}',
  '  {"ws" : [ 1 , 2 ] }  ',
  '{"__proto__":{"x":1},"constructor":1}',
];
const cases = inputs.map((input) => {
  const v = JSON.parse(input);
  return { input, compact: JSON.stringify(v), pretty: JSON.stringify(v, null, 2) };
});
const bad = ['', '{', '{"a":1,}', '[1,]', "{'a':1}", 'NaN', '01', '{"a":1} x', '"\t"', 'undefined', '.5'];
for (const b of bad) {
  try { JSON.parse(b); throw new Error(`accepted ${b}`); } catch (e) { if (e.message.startsWith('accepted')) throw e; }
}
fs.writeFileSync('cases.json', JSON.stringify({ cases, bad }, null, 2) + '\n');
