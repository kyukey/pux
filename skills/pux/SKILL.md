pux
pux saves shell commands under short names and runs them by name. Each command is a plain text file in one folder. Read this whole file before running any pux command for the user.

Before you start
Check that pux is installed with pux version. If it is missing, tell the user and point them to https://github.com/kyukey/pux. Do not install it yourself unless they ask.
Look at what already exists with pux ls, or pux find <word> to search by any part of a name. Never guess that a name is free.
Note which system and shell you are in. Quoting, multi-line input and the language of a saved command all depend on it, as the sections below show.
Saving a new command
Always pass the command as a quoted argument or through a pipe. You usually have no real terminal, and without a command pux tries to open its editor, which fails in that case.

One line, in single quotes on macOS and Linux:

```sh

pux new gs 'git status -sb'
Single quotes matter. With double quotes the shell fills in $1, $HOME and other variables at save time, and the saved command loses them. When the command itself contains a single quote, use a heredoc instead.

Several lines, through a quoted heredoc. The quotes around EOF stop the shell from changing anything inside:

```sh

pux new deploy <<'EOF'
cd ~/app
git pull
make build
EOF
Another language, with a shebang on the first line:

```sh

pux new pyhello <<'EOF'
#!/usr/bin/env python3
import sys
print("hello", *sys.argv[1:])
EOF
Without a shebang the command runs with sh on macOS and Linux, and with cmd on Windows.

Saving on Windows
A command without a shebang runs as a cmd batch script, so write its lines in cmd syntax, not sh.

One line. In cmd, single quotes are not quotes, so use double quotes. They work in PowerShell too:

```bat

pux new hi "echo hi %1"
Several lines in PowerShell, through a single-quoted here-string, which keeps everything inside as written. The closing '@ must start its line:

```powershell

@'
cd app
git pull
'@ | pux new deploy
Several lines in cmd. cmd has no heredoc, so write the command to a temp file first, then pipe it:

```bat

type deploy.cmd | pux new deploy
Shebangs work on Windows when the interpreter is on PATH. Many Python installs only provide python, so use #!/usr/bin/env python there. #!/bin/bash finds bash from Git for Windows when it is installed.

Arguments
Words after the name are passed to the command: pux deploy prod makes prod the first argument.

In sh, use $1, $2 and so on, or "$@" for all of them. Quote them, as in "$1".
In Windows cmd, use %1, %2 and so on, or %* for all of them.
In a shebang script, read them the way that language does, like sys.argv in Python.
Names
Use lowercase letters, digits, ., _ and -, starting with a letter or digit, up to 64 characters.
A name cannot end with a dot.
Prefer short, clear names joined with dashes, like docker-clean or db-backup.
Names of pux commands are reserved: new, edit, rm, ls, find, cat, where, help, version.
Names Windows refuses for files are reserved on every system: con, prn, aux, nul, com1 to com9 and lpt1 to lpt9, even with an extension like nul.txt.
Changing a command
Only use edit when the user asks to change an existing command. edit replaces the whole command, so first run pux cat <name>, show the user what will change, and then save the full new version. It needs the exact name when given the new command:

```sh

pux edit deploy <<'EOF'
cd ~/app
git pull --rebase
make build
EOF
pux new refuses a name that is taken and pux edit refuses one that does not exist. If new reports that a name exists, ask the user whether to change that command or pick another name. Never switch to edit on your own.

Checking your work
After saving, run pux cat <name> and confirm the output matches what you meant to save. cat prints the command exactly as stored.

Running, listing and deleting
pux <name> [args...] runs a command, and its exit code is the command's own.
pux ls [prefix] and pux find [text] print one name per line when run without a terminal, as you usually are. Without a terminal, find only prints the matches and never runs one.
pux rm -y <name> deletes a command. Only delete when the user asks for that exact command to go.
pux where shows where the binary and the saved commands live.
Use full names. When a start like pux dep matches several commands, pux lists them and exits with 1 instead of running anything.

Exit codes
| Code | Meaning | |---|---| | 0 | done | | 1 | name not found, ambiguous or already taken, or another error | | 2 | wrong usage or a bad name | | 127 | the interpreter in the first line is not installed | | 130 | cancelled | | other | the saved command's own exit code |

Safety
Show the user the command and the name before you save it, and say what it will do.
Do not save passwords, tokens or other secrets inside a command. Read them from an environment variable or a file the user controls instead.
Do not run a command right after saving it unless the user asks. Saving and running are separate steps.
Be careful with commands that delete files, force-push, or change system settings. Point out the risk and let the user decide.
