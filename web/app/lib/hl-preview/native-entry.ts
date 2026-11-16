/* eslint-disable @typescript-eslint/no-explicit-any -- the page's own window and the bridge core are untyped */
/**
 * The agent's hands in the desktop app's own pane browser: the site runs in a webview of the person's
 * computer, and the app's shell evaluates `__hlNativeAct.start(...)` there, then reads the outcome with `take(id)`.
 * Built by vite as one self-contained classic script; the shell injects it before any script of the site runs. It runs
 * only where the shell marked the document (`__HL_NATIVE_PANE__`) and only in the top document: a frame of the site
 * gets nothing.
 */
import * as functions from './bridge-core';
import { installNativeActs } from './native-acts';

const win = window as any;
if (win.__HL_NATIVE_PANE__ === true && win.top === win) installNativeActs(win, document, functions);
