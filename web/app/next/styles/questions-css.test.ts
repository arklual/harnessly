import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// The questions to people (asks.css) once took `.hx-ask`, `.hx-ask__q` and `@keyframes hx-ask-in` too: the card over
// the field got a width of 88%, a rule at its left and a headline-sized question. A name of the card is its sheet's alone
// (base.css's touch targets and the tabs' scroll shadows shared with other strips aside).
describe('the questions card over the field', () => {
  it('has its class names and keyframes to itself: no other sheet styles .hx-ask or its parts', () => {
    const owners = readdirSync(__dirname)
      .filter(
        (name) => name.endsWith('.css') && !['questions.css', 'composer.css', 'base.css'].includes(name),
      )
      .filter((name) =>
        /\.hx-ask(?![\w-])|\.hx-ask__(?!tabs(?![\w-]))|\.hx-ask-bar|@keyframes hx-ask-/.test(
          readFileSync(join(__dirname, name), 'utf8'),
        ),
      );
    expect(owners).toEqual([]);
  });
});
