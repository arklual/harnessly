// The little of jsdom the frame rig uses (the package ships no types, and @types/jsdom is not a dependency).
declare module 'jsdom' {
  export class JSDOM {
    constructor(
      html?: string,
      options?: { runScripts?: 'outside-only' | 'dangerously'; pretendToBeVisual?: boolean; url?: string },
    );
    readonly window: Window & typeof globalThis & { eval: (source: string) => unknown };
  }
}
