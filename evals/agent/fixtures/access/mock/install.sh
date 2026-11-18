#!/bin/sh
# Puts the mocked team infrastructure in place: the CLIs on PATH, the machine's identity in the home, the skills for
# both agents. Run from the workspace by the scenario's setup; leaves nothing of itself there.
set -e
mkdir -p "$HOME/.local/bin" "$HOME/.machine" "$HOME/.mock" .agents
for f in kubectl tsh flags grafana; do cp "mock/$f" "$HOME/.local/bin/$f"; chmod 755 "$HOME/.local/bin/$f"; done
printf 'apiVersion: v1\nkind: Config\ncurrent-context: agent-machine\n' > "$HOME/.machine/kube.yaml"
printf 'machine identity (mock)\n' > "$HOME/.machine/identity"
chmod 600 "$HOME/.machine/kube.yaml" "$HOME/.machine/identity"
cp -R .claude/skills .agents/
cp CLAUDE.md AGENTS.md
rm -rf mock
