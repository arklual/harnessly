// What the Extensions page asks the runtime for the lifecycle of an extension (cmd/runtime/routes_apps_ext.go): the
// list with the settings form and the state of the secrets (never their values), inspect and install a folder, switch
// off and on, remove with or without the data. The routes answer 404 `off` unless HARNESSLY_UI_SLOTS=on.
import { useCallback, useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export type TExtKind = 'ui' | 'runtime' | 'tool';

export interface IExtSecret {
  name: string;
  description?: string;
  optional?: boolean;
  /** Whether a value is in the safe; the value itself never comes out. */
  set: boolean;
}

export interface IExtIssue {
  warning?: boolean;
  message: string;
}

export interface ISettingField {
  type?: 'string' | 'boolean' | 'number' | 'integer';
  title?: string;
  description?: string;
  enum?: (string | number)[];
  minimum?: number;
  maximum?: number;
}

export interface ISettingsSchema {
  properties?: Record<string, ISettingField>;
  required?: string[];
}

export interface IExtension {
  id: string;
  name: string;
  description: string;
  version: string;
  previous?: string;
  kind: TExtKind;
  trust: string;
  targets: string[];
  permissions: string[];
  disabled: boolean;
  team?: boolean;
  dev?: boolean;
  settings?: ISettingsSchema;
  values?: Record<string, unknown>;
  secrets: IExtSecret[];
  dataVersion: number;
  dataTarget: number;
  compat?: IExtIssue[];
}

export interface IInspected {
  package?: IExtension;
  installed?: string;
  problems: { path?: string; message: string; warning?: boolean }[];
  host: string;
  blocked?: string;
}

const base = '/extensions' as const;
const enc = encodeURIComponent;

export const listExtensions = (m: MachineId) =>
  runtimeJson<{ extensions: IExtension[]; host: string }>(m, platformPath(base));

export const inspectExtension = (m: MachineId, dir: string) =>
  runtimeJson<IInspected>(m, platformPath(`${base}/inspect`), postJson({ dir }));

export const installExtension = (m: MachineId, dir: string) =>
  runtimeJson<{ extension: IExtension }>(m, platformPath(`${base}/install`), postJson({ dir }));

export const switchExtension = (m: MachineId, id: string, off: boolean) =>
  runtimeJson<{ extension: IExtension }>(
    m,
    platformPath(`${base}/${enc(id)}/${off ? 'disable' : 'enable'}`),
    postJson({}),
  );

export const removeExtension = (m: MachineId, id: string, purge: boolean) =>
  runtimeJson<{ id: string }>(m, platformPath(`${base}/${enc(id)}/remove`), postJson({ purge }));

export const saveSettings = (m: MachineId, id: string, values: Record<string, unknown>) =>
  runtimeJson<{ extension: IExtension }>(m, platformPath(`${base}/${enc(id)}/settings`), {
    ...postJson({ values }),
    method: 'PUT',
  });

export const putSecret = (m: MachineId, id: string, name: string, value: string) =>
  runtimeJson<{ extension: IExtension }>(m, platformPath(`${base}/${enc(id)}/secrets/${enc(name)}`), {
    ...postJson({ value }),
    method: 'PUT',
  });

export const deleteSecret = (m: MachineId, id: string, name: string) =>
  runtimeJson<{ extension: IExtension }>(m, platformPath(`${base}/${enc(id)}/secrets/${enc(name)}`), {
    method: 'DELETE',
  });

/** The extensions of the machine; `list` stays null while the runtime has not answered or has the slots off. */
export const useExtensions = (m: MachineId) => {
  const [list, setList] = useState<IExtension[] | null>(null);
  const [host, setHost] = useState('');
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    listExtensions(m).then(
      (r) => {
        if (!live) return;
        setList(r.extensions);
        setHost(r.host);
      },
      () => live && setList(null),
    );
    return () => {
      live = false;
    };
  }, [m, tick]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { list, host, reload };
};
