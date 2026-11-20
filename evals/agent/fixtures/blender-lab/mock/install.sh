#!/bin/sh
# the blender-lab skill and a project folder on the stand, `blender` on PATH as a mock. `mock/variant` is the setup
# under test: before (the skill as it was, plain blender), text (the skill with the step in its workflow, run by hand), wrap (plain skill,
# `blender` is the wrapper that shows saved scenes in Sinopia), both. Run from the workspace by the scenario's setup.
set -e
V=$(cat mock/variant)
LAB="$PWD/blender-lab"
mv lab "$LAB"
mkdir -p "$HOME/.local/bin" "$HOME/opt/blender" .claude/skills/blender-lab
cp mock/blender "$HOME/opt/blender/blender"; chmod 755 "$HOME/opt/blender/blender"
case $V in both) SK=mock/skill.after.md ;; text) SK=mock/skill.text.md ;; *) SK=mock/skill.before.md ;; esac
sed "s|@LAB@|$LAB|g" "$SK" > .claude/skills/blender-lab/SKILL.md
case $V in
  wrap|both)
    cp mock/blender_wrap.sh "$LAB/tools/blender_wrap.sh"; chmod 755 "$LAB/tools/blender_wrap.sh"
    ln -sf "$LAB/tools/blender_wrap.sh" "$HOME/.local/bin/blender" ;;
  *) ln -sf "$HOME/opt/blender/blender" "$HOME/.local/bin/blender" ;;
esac
cp -R .claude/skills .agents/ 2>/dev/null || { mkdir -p .agents; cp -R .claude/skills .agents/; }
rm -rf mock
