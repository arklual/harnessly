#!/bin/bash
# A stand-in for Claude Code's terminal: an input box on a tmux pane, a transcript on disk, the hooks it was given in
# its --settings file. A message typed into the box runs a turn of about a second and a half; "perm" in it makes the
# turn ask permission for `rm -rf build` first (drawn as Claude 2.1.311 does: the command in a gutter box), and "ask" makes
# it end with a reply that names the pending menu's choice.
# "bg" in a message makes its turn start a background shell (a `sleep` writing to $HOME/bgtasks/<id>.output, as Claude's
# run_in_background does), and when that process ends the transcript gets the task notification.
# $HOME/fake-claude-boot-ms (a number; the pane shell keeps no other variables of the runtime): a TUI that draws its box at once but takes no keys for n ms, as a real one does while it starts: what is
# typed in that time is not shown and its Enter is lost, and the text appears in the box, unsubmitted, when the time is up.
# "/model x" and "/effort x" typed into the box print Claude's «Set model to x» line (and log it in models.log); "/model opus"
# asks «Switch model?» first, answered by Enter on "1. Yes".
if [ "$1" = "--version" ]; then echo "2.1.99 (Claude Code)"; exit 0; fi
# tmux names a pane's command by argv[0]; a script would show up as the shell and look like a Claude that exited.
if [ -z "$FAKE_CLAUDE_EXEC" ]; then export FAKE_CLAUDE_EXEC=1; exec -a claude /bin/bash "$0" "$@"; fi
echo "$*" >> "$HOME/claude-calls.log"
settings=""; sid=""; prompt=""
while [ $# -gt 0 ]; do
  case "$1" in
    --settings) settings="$2"; shift ;;
    --resume) sid="$2"; shift ;;
    # Everything after "--" is the first prompt, which the agent submits as soon as it is up, as the real one does.
    --) shift; prompt=$(printf '%s' "$*" | tr '\n' ' '); break ;;
  esac
  shift
done
[ -n "$sid" ] || sid=$(cat /proc/sys/kernel/random/uuid)
cwd=$(pwd)
project="$HOME/.claude/projects/$(printf '%s' "$cwd" | sed 's/[^A-Za-z0-9]/-/g')"
mkdir -p "$project" "$HOME/.claude/sessions"
transcript="$project/$sid.jsonl"
printf '{"pid":%s,"sessionId":"%s","cwd":"%s","startedAt":%s000}\n' "$$" "$sid" "$cwd" "$(date +%s)" > "$HOME/.claude/sessions/$$.json"
: >> "$transcript"
n=0
stamp() { date -u +%Y-%m-%dT%H:%M:%S.000Z; }
write() { printf '%s\n' "$1" >> "$transcript"; }
hook() {
  sed -n 's/^ *"command": "\(.*\)",\{0,1\}$/\1/p' "$settings" | sed 's/\\"/"/g' | grep -- "$1\$" | head -1
}
fire() { ( echo "{\"session_id\":\"$sid\",\"notification_type\":\"$2\"}" | eval "$(hook "$1")" ) </dev/null >/dev/null 2>&1 & }
entry() { # role text
  n=$((n+1))
  if [ "$1" = user ]; then
    write "$(printf '{"type":"user","message":{"role":"user","content":%s},"uuid":"u-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$(printf '%s' "$2" | sed 's/\\/\\\\/g; s/"/\\"/g; s/^/"/; s/$/"/')" "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
  else
    write "$(printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%s}]},"uuid":"a-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$(printf '%s' "$2" | sed 's/\\/\\\\/g; s/"/\\"/g; s/^/"/; s/$/"/')" "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
  fi
}
# the call Claude is asking permission for, and its answer, as the transcript has them
call() {
  n=$((n+1)); tool="toolu_fake_$n"
  write "$(printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"%s","name":"Bash","input":{"command":"rm -rf build","description":"Remove the build output"}}]},"uuid":"a-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$tool" "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
}
result() { # is_error text
  n=$((n+1))
  write "$(printf '{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"%s","is_error":%s,"content":"%s"}]},"uuid":"u-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$tool" "$1" "$2" "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
}
bgcall() { # the Bash call Claude makes with run_in_background, and its result naming the task and its output file
  n=$((n+1)); tool="toolu_bg_$n"; bgid="bg$n"; bgout="$HOME/bgtasks/$bgid.output"
  mkdir -p "$HOME/bgtasks"; : > "$bgout"
  write "$(printf '{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"%s","name":"Bash","input":{"command":"npm run dev","description":"dev server","run_in_background":true}}]},"uuid":"a-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$tool" "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
  ( exec -a bgjob sleep 120 ) > "$bgout" 2>&1 &
  bgpid=$!
  result false "Command running in background with ID: $bgid. Output is being written to: $bgout"
}
bgwatch() { # the background shell ended: Claude says so in its transcript
  if [ -n "$bgpid" ] && ! kill -0 "$bgpid" 2>/dev/null; then
    n=$((n+1))
    write "$(printf '{"type":"queue-operation","operation":"enqueue","timestamp":"%s","sessionId":"%s","content":"<task-notification><task-id>%s</task-id><status>killed</status></task-notification>"}' "$(stamp)" "$sid" "$bgid")"
    bgpid=""
    draw
  fi
}
trap '[ -n "$bgpid" ] && kill "$bgpid" 2>/dev/null' EXIT
bgpid=""; bgid=""; bgout=""
tool=""
rule=$(printf '─%.0s' $(seq 1 80))
buf=""; busy=0; until=0; menu=0; shown=""; pasting=0; last=""; confirm=0; target=""
draw() {
  printf '\033[H\033[2J'
  printf '  \033[1mClaude Code\033[0m v2.1.99\n  fake claude ready %s\n\n' "$sid"
  [ -n "$shown" ] && printf '%s\n' "$shown"
  if [ $confirm = 1 ]; then
    printf '%s\n   Switch model?\n   Your next response will be slower and use more tokens\n\n   This conversation is cached for the current model. Switching to %s means the full history gets re-read on\n   your next message.\n\n   ❯ 1. Yes, switch to %s\n     2. No, go back\n' "$rule" "$target" "$target"
    return
  fi
  if [ $menu = 1 ]; then
    printf '%s\n Bash command\n Remove the build output\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n │ rm -rf build\n ╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, and don'"'"'t ask again for rm commands in %s\n   3. No, and tell Claude what to do differently (esc)\n' "$rule" "$cwd"
    return
  fi
  [ $busy = 1 ] && printf '\n✻ Working… (esc to interrupt)\n'
  printf '\n%s\n\033[39m❯ \033[0m%s\n%s\n' "$rule" "$buf" "$rule"
  printf '  \033[38;5;246m? for shortcuts%s\033[39m\n' "${bgpid:+ · 1 shell}"
}
now() { echo $(( $(date +%s%N) / 1000000 )); }
turnend() { # the line Claude writes when a turn is over, before its Stop hook runs
  n=$((n+1))
  write "$(printf '{"type":"system","subtype":"stop_hook_summary","uuid":"s-%s-%s","timestamp":"%s","sessionId":"%s","cwd":"%s"}' "$sid" "$n" "$(stamp)" "$sid" "$cwd")"
}
finish() { # reply
  entry assistant "$1"; turnend; shown="● $1"; busy=0; fire stop ""
}
sleep 0.3
fire session-start ""
draw
booting=0; pend=""; bootend=0
boot_ms=$(cat "$HOME/fake-claude-boot-ms" 2>/dev/null || echo 0)
if [ "$boot_ms" -gt 0 ]; then booting=1; bootend=$(( $(now) + boot_ms )); fi
while :; do
  if [ $booting = 1 ] && [ "$(now)" -ge $bootend ]; then booting=0; buf="$pend"; pend=""; draw; fi
  if [ $booting = 0 ] && [ -n "$prompt" ]; then
    last="$prompt"; prompt=""; shown=""; entry user "$last"; fire prompt-submit ""
    busy=1; until=$(( $(now) + 1500 )); draw
  fi
  IFS= read -rsn1 -t 0.2 ch; rc=$?
  # a timeout in the middle of a multi-byte character hands over the bytes read so far
  if [ $rc -gt 128 ] && [ -z "$ch" ]; then
    bgwatch
    if [ $busy = 1 ] && [ $menu = 0 ] && [ "$(now)" -ge $until ]; then
      case "$last" in *bg*) bgcall; finish "started: $last" ;; *perm*) menu=1; entry assistant "I will clean the build folder."; call; fire notification permission_prompt ;; *) finish "done: $last" ;; esac
      draw
    fi
    continue
  fi
  if [ $booting = 1 ]; then
    if [ "$ch" = $'\e' ]; then IFS= read -rsn5 -t 0.05 rest; elif [ -n "$ch" ]; then pend="$pend$ch"; fi
    continue
  fi
  if [ $menu = 1 ]; then
    case "$ch" in
      1|2) menu=0; result false "removed"; busy=1; until=$(( $(now) + 800 )); last="resumed"; shown="● running rm -rf build" ;;
      3) menu=0; result true "The user doesn't want to proceed"; finish "denied: $last" ;;
    esac
    draw; continue
  fi
  if [ $confirm = 1 ]; then
    if [ "$ch" = $'\e' ]; then IFS= read -rsn5 -t 0.05 rest; ch=2; fi
    case "$ch" in
      "") confirm=0; shown="  ⎿  Set model to $target"; echo "model $target" >> "$HOME/models.log" ;;
      2) confirm=0; shown="  ⎿  Kept model as before" ;;
    esac
    draw; continue
  fi
  if [ "$ch" = $'\e' ]; then
    IFS= read -rsn5 -t 0.05 rest
    case "$rest" in
      "[200~") pasting=1 ;;
      "[201~") pasting=0 ;;
      "") if [ $busy = 1 ]; then busy=0; entry assistant "[interrupted]"; shown="● interrupted"; fire stop ""; fi ;;
    esac
    draw; continue
  fi
  if [ -z "$ch" ]; then # Enter
    if [ $pasting = 1 ]; then buf="$buf "; draw; continue; fi
    case "$buf" in
      "/model "*) target="${buf#/model }"; buf=""; shown=""
        if [ "$target" = opus ]; then confirm=1; else shown="  ⎿  Set model to $target"; echo "model $target" >> "$HOME/models.log"; fi
        draw; continue ;;
      "/effort "*) shown="  ⎿  Set effort level to ${buf#/effort }"; echo "effort ${buf#/effort }" >> "$HOME/models.log"; buf=""
        draw; continue ;;
    esac
    if [ -n "$buf" ]; then
      last="$buf"; buf=""; shown=""; entry user "$last"; fire prompt-submit ""
      busy=1; until=$(( $(now) + 1500 ))
    fi
    draw; continue
  fi
  buf="$buf$ch"; draw
done
