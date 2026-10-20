// What Codex asks the person to approve, as the runtime keeps it on the tab (internal/agents/codex/hook.go).
export interface IPatchEntry {
  path: string;
  operation: string;
  diff?: string;
  content?: string;
}

interface IBaseRequest {
  callId?: string;
  cwd?: string;
  env?: Record<string, string>;
}

export interface IExecApprovalRequest extends IBaseRequest {
  type: 'ExecApprovalRequest';
  command: string;
}

export interface IApplyPatchApprovalRequest extends IBaseRequest {
  type: 'ApplyPatchApprovalRequest';
  patches: IPatchEntry[];
}

export interface IRequestPermissions extends IBaseRequest {
  type: 'RequestPermissions';
  permissions: string[];
}

export type IPermissionRequest = IExecApprovalRequest | IApplyPatchApprovalRequest | IRequestPermissions;
