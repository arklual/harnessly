# Video: the check

The check runs at any scale, and you say "ready" only after it. A green check does not mean it is beautiful: you look at the frames too.

1. **After every edit:** `{{cli}} design video check <id> --scene "<scene>"`. It finds timers and own frame loops, outside addresses, moments past the
   end, two tweens on one property at one time (`motion-overlap`), engine and script errors, assets that failed, motion longer than the scene, and
   walks the scene in time for overflowing text and blocks off the screen in every format. It also compares a frame reached by seeking with
   the same frame reached another way. Errors are not optional: fix them.
2. **Look at the sheet.** `{{cli}} design video frame <id> "<scene>" --sheet` prints one picture with a frame per half second and the times on it (add
   `--format 9:16`). Open it with Read. Against the script, ask of each: is the idea readable in this frame, does anything collide, does the eye know
   where to look, does the motion land on the moment, is there an empty frame. Zoom into a doubt with `--at 2.4s`.
3. **Draft.** Before you say it is ready: `{{cli}} design video render <id> --draft --wait`.
4. **Fix at most twice** by yourself: after that show the person what you see and let them decide.
5. **Tell the truth in the answer:** what you checked, what you saw, what remains. A comment is done only after you looked at its moment.
