# Video: other engines

`tkVideo.register` puts anything on the scene's time, and engines mix in one scene. Choose by the scene: 2D choreography is GSAP
(`{{cli}} design video guide motion`), particles and light are a canvas or a shader, volume is 3D, many colliding bodies are physics.

```js
tkVideo.register({
  name: 'particles',
  ready: function () { return loadWhatItNeeds(); },   // optional: the scene waits for this promise
  seek: function (ms) { draw(ms / 1000); },           // draws time ms; false means nothing changed
});
```

An engine must draw from the time alone, with no clock and no memory of the previous frame: `seek` is called in any order, and also backwards.

- **Canvas 2D.** Draw the whole frame in `seek`. A particle is a formula of time and its seed (`x = x0 + vx * t`), not a value moved each frame.
- **WebGL and shaders.** Pass `u_time` to the shader and draw in `seek`. Create the context with `preserveDrawingBuffer: true`, or the capture gets
  a blank frame. There is no GPU on the machine: the frame is counted by the CPU, so keep the shader simple and measure one frame first.
- **3D, Lottie and physics libraries** are not in the kit yet. Put the file you need in the project and load it from there only if the person
  asked for it, and say that the render may be slow. Physics: count the poses once at a fixed step in `ready`, keep them in a table, and let
  `seek` read the table.
- GSAP may drive the numbers of any engine: a tween of `state.u_mix` inside `tkVideo.motion`, read by `seek`.
- No engine reaches the network: the check fails on any outside address.
