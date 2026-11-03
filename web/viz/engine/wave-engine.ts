// SPDX-License-Identifier: Apache-2.0
import type { VizViewport } from '../model/viewport';
import { VizTimeEngine } from './time-engine';
import { WAVE_RENDERER } from './wave-paint';

/** Волновой график: монотонные кривые с градиентом, точки, перекрестье. */
export class WaveEngine extends VizTimeEngine {
  constructor(canvas: HTMLCanvasElement, viewport: VizViewport) {
    super(canvas, viewport, WAVE_RENDERER);
  }
}
