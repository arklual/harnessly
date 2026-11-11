// An example wrapper for React with react-hook-form: the form becomes a state the agent reads and patches.
// Copy it into the shared package of your shell; window.harnessly is the stand-in of the loader line outside Harnessly.
import { useEffect, useRef } from 'react';
import type { FieldValues, Path, UseFormReturn } from 'react-hook-form';
import type { ActionOptions, IssueInput, StateOptions } from '@harnessly/app-sdk';

type FormOptions = Pick<StateOptions, 'description' | 'schema' | 'rules' | 'check'>;

// formState.errors as issues, read through getFieldState: it is the live one, where the formState of a render is a
// snapshot taken before trigger() finished. Top-level fields; walk the error object the same way for nested ones.
const issuesOf = <T extends FieldValues>(form: UseFormReturn<T>): IssueInput[] =>
  Object.keys(form.getValues()).flatMap((field) => {
    const message = form.getFieldState(field as Path<T>).error?.message;
    return typeof message === 'string' ? [{ source: 'form' as const, field, message }] : [];
  });

export function useAgentForm<T extends FieldValues>(name: string, form: UseFormReturn<T>, options: FormOptions): void {
  // The latest form and options without registering again on every render.
  const latest = useRef({ form, options });
  latest.current = { form, options };
  useEffect(() => {
    const handle = window.harnessly.state(name, {
      ...latest.current.options,
      get: () => latest.current.form.getValues(),
      set: async (patch) => {
        const { getValues, reset, trigger } = latest.current.form;
        reset({ ...getValues(), ...patch }, { keepDefaultValues: true, keepDirty: true });
        await trigger();
        return issuesOf(latest.current.form);
      },
      subscribe: (changed) => latest.current.form.watch(changed),
    });
    return () => handle.unregister();
  }, [name]);
}

export function useAgentAction(name: string, options: ActionOptions): void {
  const latest = useRef(options);
  latest.current = options;
  useEffect(() => {
    const handle = window.harnessly.action(name, { ...latest.current, run: (args) => latest.current.run(args) });
    return () => handle.unregister();
  }, [name]);
}
