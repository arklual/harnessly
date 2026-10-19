// The process table of the scenario.
import { world } from './stub-world.mjs';

const proc = (pid) => world().procs[String(pid)];
export const isProcessRunning = async (pid) => proc(pid) !== undefined;
export const getChildPids = async (pid) => [...(proc(pid)?.children ?? [])];
export const getProcessArgs = async (pid) => proc(pid)?.args ?? null;
export const getProcessCwd = async (pid) => proc(pid)?.cwd ?? null;
export const getProcessStartTimeMs = async () => null;
