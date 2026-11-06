// The connection adapter of the pack (docs/connection-adapters.md, protocol harnessly.connection/v1): the issue tracker
// of Example Corp on tracker.example.com. It keeps no credentials: the runtime sends the person's token, taken from its
// safe, in the body of each call; here any token equal to EXAMPLE_TRACKER_TOKEN (default "demo") is accepted.
//
//   ADAPTER_TOKEN=secret PORT=8082 node --experimental-strip-types connection.ts
import { adapter, fail, isMain, listen } from "./lib.ts";

const HOST = "tracker.example.com";

const issues = [
  {
    key: "EX-1",
    title: "Connect the tracker",
    status: { name: "Open", category: "todo" },
    description: "A first issue.",
  },
  {
    key: "EX-2",
    title: "Write the docs",
    status: { name: "Done", category: "done" },
    description: "A second issue.",
  },
].map((i) => ({ ...i, url: `https://${HOST}/issue/${i.key}`, project: "EX" }));

const pages = [
  {
    id: "1",
    title: "Onboarding",
    url: `https://${HOST}/wiki/1`,
    space: "EX",
    body: "Welcome to Example Corp.",
  },
];

export function connection(token = "", trackerToken = "demo") {
  const denied = (b: any): [number, unknown] | undefined =>
    b.credentials?.token === trackerToken
      ? undefined
      : fail(401, "denied", "the token is not accepted");
  return adapter({
    description: {
      protocol: "harnessly.connection/v1",
      name: "example-tracker",
      version: "1.0.0",
      capabilities: ["detect", "link", "issues", "pages"],
    },
    token,
    routes: {
      "POST /v1/detect": (b) => {
        let u: URL;
        try {
          u = new URL(b.url);
        } catch {
          return fail(404, "not_found");
        }
        if (u.hostname !== HOST) return fail(404, "not_found");
        return [
          200,
          {
            baseUrl: `https://${HOST}`,
            deployment: "self-hosted",
            title: "Example tracker",
            methods: [
              {
                id: "pat",
                recommended: true,
                fields: [{ id: "token", secret: true }],
              },
            ],
            capabilities: ["issues", "pages"],
          },
        ];
      },
      "POST /v1/link": (b) => {
        const m = /\/issue\/([A-Z]+-\d+)/.exec(String(b.url));
        return [
          200,
          m ? { found: true, kind: "issue", key: m[1] } : { found: false },
        ];
      },
      "POST /v1/me": (b) =>
        denied(b) ?? [
          200,
          { login: "demo", name: "Demo User", email: "demo@example.com" },
        ],
      "POST /v1/projects": (b) =>
        denied(b) ?? [
          200,
          {
            projects: [
              { key: "EX", name: "Example", url: `https://${HOST}/project/EX` },
            ],
          },
        ],
      "POST /v1/issues/search": (b) => {
        const no = denied(b);
        if (no) return no;
        const text = String(b.query?.text ?? "").toLowerCase();
        const found = issues
          .filter((i) => i.title.toLowerCase().includes(text))
          .map(({ description: _d, ...i }) => i);
        return [200, { issues: found.slice(0, b.query?.limit ?? 50) }];
      },
      "POST /v1/issues/get": (b) => {
        const no = denied(b);
        if (no) return no;
        const one = issues.find((i) => i.key === b.key);
        return one ? [200, one] : fail(404, "not_found");
      },
      "POST /v1/pages/search": (b) => {
        const no = denied(b);
        if (no) return no;
        const text = String(b.query?.text ?? "").toLowerCase();
        return [
          200,
          {
            pages: pages
              .filter((p) => p.title.toLowerCase().includes(text))
              .map(({ body: _b, ...p }) => p),
          },
        ];
      },
      "POST /v1/pages/get": (b) => {
        const no = denied(b);
        if (no) return no;
        const one = pages.find((p) => p.id === b.id);
        return one ? [200, one] : fail(404, "not_found");
      },
    },
  });
}

if (isMain(import.meta.url))
  listen(
    connection(process.env.ADAPTER_TOKEN, process.env.EXAMPLE_TRACKER_TOKEN),
    "example-tracker",
  );
