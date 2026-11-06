// The «Инструменты» section's contract: which MCP servers, skills and built-in tools the agents call, by day, with the
// errors and the people behind the calls (`GET …/tools`, internal/hub/analytics/tools.go).
export type ToolKind = 'mcp' | 'skill' | 'builtin' | string;

export interface ToolCaller {
  userId: string;
  name: string;
  calls: number;
  errors: number;
}

export interface ToolItem {
  kind: ToolKind;
  name: string;
  itemId?: string;
  calls: number;
  errors: number;
  users: number;
  teams: number;
  /** Unix milliseconds of the last call. */
  lastAt: number;
  /** Calls by day, in the order of `ToolsData.days`. */
  days: number[];
  people: ToolCaller[];
}

export interface ToolsData {
  /** The period's days, `YYYY-MM-DD`. */
  days: string[];
  calls: number;
  items: ToolItem[];
}

export interface ToolsApi {
  /** The days from `from` up to (not including) `to`. */
  toolsList(org: string, arg: { from: string; to: string }): Promise<ToolsData>;
}
