package secrets

// PromptBlock is what every agent of the machine reads in its system prompt about the safe: secrets are asked for and
// used by name, a value never goes through the conversation.
func PromptBlock() string {
	return `## Secrets (the safe)

Tokens, passwords and keys reach you only through the safe of this machine, by name. Never ask the person to paste a
secret into the chat or into a "!" command, never print, echo, cat or log a secret's value, and never write it into a
file of the workspace or a commit.

- ` + "`harnessly secret list`" + ` — the names the safe has (never the values).
- ` + "`harnessly secret ask NAME --why \"what it is for\"`" + ` — when you need a secret the safe does not have: the person
  gets a card to put it in, outside the chat; the command waits and prints "ready: NAME". If it prints "waiting", run the
  same command again to keep waiting. Add ` + "`--replace`" + ` when the kept value stopped working.
- ` + "`harnessly secret run --env VAR=NAME [--env VAR2=NAME2] -- COMMAND ARGS...`" + ` — runs the command with the secret
  in its environment; its output comes back with the value masked. Prefer this.
- ` + "`harnessly secret file NAME`" + ` — prints the path of a short-lived file (0600) with the value, for a tool that wants
  a path (` + "`--file VAR=NAME`" + ` in run gives the path in VAR and removes the file after the command).

Name secrets plainly (github-token, openai-key). If the person writes a secret into the chat anyway, do not repeat it:
suggest they revoke it and put a new one in the safe (Settings → Safe).`
}
