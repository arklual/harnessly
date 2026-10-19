// The stored tab's session is the scenario's answer; finding a transcript by id is the real lookup.
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import { world, record } from './stub-world.mjs';

export const findClaudeTranscript = async (sessionId) => {
  const projects = path.join(os.homedir(), '.claude', 'projects');
  const dirs = await fs.readdir(projects).catch(() => []);
  const found = await Promise.all(dirs.map(async (dir) => {
    const file = path.join(projects, dir, `${sessionId}.jsonl`);
    return (await fs.stat(file).catch(() => null)) ? file : null;
  }));
  return found.find((file) => file !== null) ?? null;
};
export const recoverAndStoreTabSession = async (session) => {
  record('recover', session);
  return world().recover[session] ?? null;
};
