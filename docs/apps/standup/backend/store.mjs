// The standup store: one JSON file a day, <dir>/YYYY-MM-DD.json, in the chat's workspace. The agent writes it through
// the MCP server, the person edits it from the view (hl.files), so both sides share one file format:
//   { "date": "2026-11-03", "entries": [ { id, person, yesterday, today, blockers, updatedAt, updatedBy } ] }
// There is deliberately no delete here: removing an entry is the person's act, done in the view.
import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

/** The machine's local date as YYYY-MM-DD (the view uses the browser's, so they agree on one machine). */
export function today(now = new Date()) {
  const p = (n) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${p(now.getMonth() + 1)}-${p(now.getDate())}`;
}

export class Store {
  constructor(dir, now = () => new Date()) {
    this.dir = dir;
    this.now = now;
  }

  file(date) {
    // The date becomes a file name: nothing but a date may get through.
    if (!DATE_RE.test(date)) throw new Error('date must be YYYY-MM-DD');
    return join(this.dir, `${date}.json`);
  }

  load(date = today(this.now())) {
    const file = this.file(date);
    if (!existsSync(file)) return { date, entries: [] };
    const doc = JSON.parse(readFileSync(file, 'utf8'));
    return { date, entries: Array.isArray(doc.entries) ? doc.entries : [] };
  }

  save(doc) {
    mkdirSync(this.dir, { recursive: true });
    const file = this.file(doc.date);
    // Written aside and renamed: a reader (the view) never sees half a file.
    const tmp = `${file}.${process.pid}.tmp`;
    writeFileSync(tmp, `${JSON.stringify(doc, null, 2)}\n`);
    renameSync(tmp, file);
  }

  /**
   * Creates the person's entry for the day, or changes the texts it is given and keeps the others.
   * The store holds the rules the instructions state, so a confused agent cannot get past them: an entry the person
   * edited changes only with the updatedAt the agent read (expectedUpdatedAt), and no call empties an entry, since
   * removing one is the person's act.
   */
  upsert({ person, yesterday, today: todayText, blockers, expectedUpdatedAt }, date) {
    const name = typeof person === 'string' ? person.trim() : '';
    if (name === '') throw new Error('person is required');
    const doc = this.load(date);
    const at = this.now().toISOString();
    const given = { yesterday, today: todayText, blockers };
    const texts = {};
    for (const key of Object.keys(given)) if (given[key] !== undefined) texts[key] = String(given[key]);
    let entry = doc.entries.find((e) => String(e.person).toLowerCase() === name.toLowerCase());
    if (entry) {
      if (entry.updatedBy === 'person' && expectedUpdatedAt !== entry.updatedAt) {
        throw new Error('the person edited this entry: read it with list_entries, ask them, then pass its updatedAt as expectedUpdatedAt');
      }
      const next = { ...entry, ...texts };
      if (!next.yesterday?.trim() && !next.today?.trim() && !next.blockers?.trim()) {
        throw new Error('this would empty the entry: only the person removes entries, in the view');
      }
      Object.assign(entry, texts, { updatedAt: at, updatedBy: 'agent' });
    } else {
      entry = { id: randomUUID(), person: name, yesterday: '', today: '', blockers: '', ...texts, updatedAt: at, updatedBy: 'agent' };
      doc.entries.push(entry);
    }
    this.save(doc);
    return entry;
  }
}
