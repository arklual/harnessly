---
name: release
description: How a service of the team is released — tags, the pipeline, the rollout order, the checks after it. Use before a release or when a rollout is stuck.
---

# Release

Tag `vX.Y.Z` on main; the pipeline builds the image and the deploy job rolls it out to stage, then prod after the checks.
