// The memory adapter of the pack (docs/memory-adapters.md, protocol harnessly.memory/v1): the agent's notes of Example
// Corp. The notes are a JSON file here; a real adapter keeps them in a wiki or a database.
//
//   ADAPTER_TOKEN=secret PORT=8081 MEMORY_FILE=/var/lib/example-corp-pack/memory.json node --experimental-strip-types memory.ts
import { adapter, fail, isMain, listen, stateFile } from "./lib.ts";

interface Stored {
  text: string;
  modifiedMs: number;
  version: number;
}

const NAME = /^[A-Za-z0-9_.-]+\.md$/;

export function memory(file?: string, token = "") {
  const notes = stateFile<Record<string, Stored>>(file, {
    "handbook.md": {
      text: "# Handbook\nOur docs live at https://docs.example.com.\n",
      modifiedMs: Date.now(),
      version: 1,
    },
  });
  const parse = (text: string) => {
    const front = /^---\n([\s\S]*?)\n---\n?/.exec(text);
    const body = (front ? text.slice(front[0].length) : text).trim();
    return {
      body,
      title: /^#\s*(.+)$/m.exec(body)?.[1]?.trim() ?? "",
      description: front
        ? (/^description:\s*(.+)$/m.exec(front[1])?.[1]?.trim() ?? "")
        : "",
    };
  };
  const entry = (name: string, s: Stored) => {
    const p = parse(s.text);
    return {
      name,
      title: p.title || name.replace(/\.md$/, ""),
      description: p.description,
      modifiedMs: s.modifiedMs,
      version: String(s.version),
    };
  };
  return adapter({
    description: {
      protocol: "harnessly.memory/v1",
      name: "example-corp-memory",
      version: "1.0.0",
      capabilities: ["read", "write", "search"],
    },
    token,
    routes: {
      "POST /v1/list": () => [
        200,
        {
          notes: Object.entries(notes.value)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([n, s]) => entry(n, s)),
        },
      ],
      "POST /v1/get": (b) => {
        const s = notes.value[String(b.name)];
        return s
          ? [200, { ...entry(b.name, s), body: parse(s.text).body }]
          : fail(404, "not_found");
      },
      "POST /v1/put": (b) => {
        const name = String(b.name);
        if (!NAME.test(name) || typeof b.text !== "string")
          return fail(400, "bad_request", "a name ending in .md and a text");
        const cur = notes.value[name];
        if (cur && b.ifVersion && b.ifVersion !== String(cur.version))
          return fail(409, "conflict", "the note changed");
        const next = {
          text: b.text,
          modifiedMs: Date.now(),
          version: (cur?.version ?? 0) + 1,
        };
        notes.value[name] = next;
        notes.save();
        return [200, entry(name, next)];
      },
      "POST /v1/search": (b) => {
        const q = String(b.query ?? "").toLowerCase();
        const limit = Number(b.limit) > 0 ? Number(b.limit) : 20;
        const hits = [];
        for (const [name, s] of Object.entries(notes.value)) {
          const line = s.text
            .split("\n")
            .find((l) => l.toLowerCase().includes(q));
          if (q && line)
            hits.push({
              ...entry(name, s),
              snippet: line.trim().slice(0, 200),
            });
          if (hits.length === limit) break;
        }
        return [200, { hits }];
      },
    },
  });
}

if (isMain(import.meta.url))
  listen(
    memory(process.env.MEMORY_FILE, process.env.ADAPTER_TOKEN),
    "example-corp-memory",
  );
