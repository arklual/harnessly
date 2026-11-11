// An example wrapper for Angular: a reactive form becomes a state the agent reads and patches. Copy it into
// the shared package of your shell; window.harnessly is the stand-in of the loader line outside Harnessly.
import { DestroyRef, inject } from '@angular/core';
import { AbstractControl, FormGroup } from '@angular/forms';
import type { ActionOptions, IssueInput, StateOptions } from '@harnessly/app-sdk';

type Messages = Record<string, (error: unknown) => string>;
type FormOptions = Pick<StateOptions, 'description' | 'schema' | 'rules' | 'check'> & { messages?: Messages };

// Every error of every control, with the text the person sees under the field.
const issuesOf = (control: AbstractControl, field: string, messages: Messages, out: IssueInput[] = []): IssueInput[] => {
  for (const [key, error] of Object.entries(control.errors ?? {})) {
    out.push({ source: 'form', field, message: messages[key]?.(error) ?? key });
  }
  if (control instanceof FormGroup) {
    for (const [name, child] of Object.entries(control.controls)) issuesOf(child, field ? `${field}.${name}` : name, messages, out);
  }
  return out;
};

/** Call in an injection context (a constructor or a field initializer): the state goes when the component does. */
export function exposeForm(name: string, form: FormGroup, { messages = {}, ...options }: FormOptions): void {
  const handle = window.harnessly.state(name, {
    ...options,
    get: () => form.getRawValue(),
    set: (patch) => {
      form.patchValue(patch);
      form.markAllAsTouched();
      form.updateValueAndValidity();
      return issuesOf(form, '', messages);
    },
    subscribe: (changed) => form.valueChanges.subscribe(changed),
  });
  inject(DestroyRef).onDestroy(() => handle.unregister());
}

export function exposeAction(name: string, options: ActionOptions): void {
  const handle = window.harnessly.action(name, options);
  inject(DestroyRef).onDestroy(() => handle.unregister());
}
