#!/bin/sh
# Builds the git repositories the GitLab golden cases walk, under $1, with fixed identities and clocks so every run
# (the generator and the Go test) gets the same commits, reflogs and branch dates.
set -eu
root=$1
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null HOME="$root/githome"
export GIT_AUTHOR_NAME=Agent GIT_AUTHOR_EMAIL=agent@example.com GIT_COMMITTER_NAME=Agent GIT_COMMITTER_EMAIL=agent@example.com
mkdir -p "$HOME"
at() { export GIT_AUTHOR_DATE="$1 +0000" GIT_COMMITTER_DATE="$1 +0000"; }
record() { at "$1"; echo "$2" >>"$3"; git add -A; git commit -q -m "$2"; }

# alpha: self-hosted GitLab, two branches with session commits, one older branch.
mkdir -p "$root/work/alpha/src/deep" && cd "$root/work/alpha"
at "2026-10-17T08:00:00"; git init -q -b main .; git remote add origin https://gitlab.example.com/group/alpha.git
record "2026-10-17T08:00:00" "initial" README.md
git branch old-work
record "2026-10-18T10:30:00" "feat: one" src/a.txt
at "2026-10-18T10:40:00"; git checkout -q -b feature/x
record "2026-10-18T10:45:00" "feat: two" src/deep/b.txt
record "2026-10-18T10:50:00" "fix: three" src/deep/b.txt
at "2026-10-18T11:00:00"; git checkout -q main
record "2026-10-18T11:05:00" "chore: four" README.md

# beta: the public GitLab over ssh, its project renamed there.
mkdir -p "$root/work/beta" && cd "$root/work/beta"
at "2026-10-16T08:00:00"; git init -q -b trunk .; git remote add origin git@gitlab.com:team/sub/beta.git
record "2026-10-16T08:00:00" "initial" notes.md
record "2026-10-18T12:00:00" "docs: session" notes.md

# gamma: a remote that is no GitLab of ours.
mkdir -p "$root/work/gamma" && cd "$root/work/gamma"
at "2026-10-16T08:00:00"; git init -q -b main .; git remote add origin https://code.example.org/x/gamma.git
record "2026-10-18T12:30:00" "work" w.txt

# delta: self-hosted GitLab, nothing done during the session; a nested repository inside it has a session commit.
mkdir -p "$root/work/delta/vendor" && cd "$root/work/delta"
at "2026-10-10T08:00:00"; git init -q -b main .; git remote add origin https://gitlab.example.com/group/delta.git
record "2026-10-10T08:00:00" "initial" d.txt
mkdir -p vendor/inner && cd vendor/inner
git init -q -b main .; git remote add origin https://gitlab.example.com/group/inner.git
record "2026-10-18T13:00:00" "inner change" i.txt

# epsilon: no reflog (as after a clone of imported work), so commits are read by date; a branch GitLab lacks.
mkdir -p "$root/work/epsilon" && cd "$root/work/epsilon"
at "2026-10-15T08:00:00"; git init -q -b main .; git remote add origin https://gitlab.example.com/group/epsilon.git
record "2026-10-15T08:00:00" "initial" e.txt
at "2026-10-18T14:00:00"; git checkout -q -b local-only
record "2026-10-18T14:10:00" "wip" e.txt
rm -rf .git/logs
