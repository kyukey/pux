# pux

Save commands, run them by name.

pux keeps the commands you type again and again in one place, so you can run them with a short name instead of digging through shell history or growing a long `.zshrc` full of aliases. It runs on macOS, Linux and Windows, and it is a single small binary with no setup.

```sh
pux new deploy 'git pull && make build && ./restart.sh'
pux deploy
```

## Install

**Homebrew** (macOS and Linux)

```sh
brew install kyukey/tap/pux
```

**Scoop** (Windows)

```sh
scoop bucket add kyukey https://github.com/kyukey/scoop-bucket
scoop install pux
```

**Go** (1.26 or newer)

```sh
go install github.com/kyukey/pux/cmd/pux@latest
```

This puts `pux` in `~/go/bin`, so make sure that folder is on your `PATH`.

**Binary**

Download the archive for your system from the [releases page](https://github.com/kyukey/pux/releases), unpack it and put `pux` somewhere on your `PATH`.

## Quick start

```sh
pux new hi 'echo hi $1'     # save a command
pux hi there                # run it, "there" becomes $1
pux ls                      # list what you saved
pux find                    # search, pick with the arrow keys, Enter runs it
pux edit hi                 # change it
pux cat hi                  # print it without running it
pux rm hi                   # delete it
```

On Windows the first line is `pux new hi "echo hi %~1"` instead. See [On Windows](#on-windows).

Run `pux` on its own for a short overview, `pux --examples` for examples that match your system, or `pux help` for everything.

## Saving commands

There are three ways to save a command, and `new` and `edit` both accept all of them.

**Quoted**, for one-liners:

```sh
pux new gs 'git status -sb'
```

On macOS and Linux, use single quotes. With double quotes your shell fills in things like `$1` or `$HOME` before pux sees them, so the saved command would not get them later.

**Piped**, for whole scripts:

```sh
cat setup.sh | pux new setup
```

**In the editor**, when you leave the command out:

```sh
pux new deploy
```

The editor opens right below your prompt. Type or paste the command, then press `Ctrl+D` to save or `Esc` to cancel. If you have unsaved changes, `Esc` asks once more before throwing them away. `Tab` and `Shift+Tab` move between the name and the command.

`new` refuses a name that is already taken, and `edit` refuses one that does not exist, so a typo never overwrites a script by accident.

## Running commands

```sh
pux deploy                  # run it
pux deploy prod --force     # extra words are passed on as $1, $2 and so on
```

A command without a first line like `#!...` runs with `sh` on macOS and Linux, and with `cmd` on Windows. Start it with a shebang to pick another interpreter:

```sh
#!/usr/bin/env python3
import sys
print("hello", sys.argv[1:])
```

The interpreter has to be installed and on your `PATH` under that name.

pux passes your command's own output and exit code straight through, so scripts can check the result as usual.

## On Windows

Commands without a shebang run with `cmd`, so arguments are `%~1`, `%~2` and so on, or `%*` to pass all of them on. Double quotes work in both cmd and PowerShell:

```bat
pux new hi "echo hi %~1"
pux hi there
```

pux hands each argument to cmd in quotes, so spaces and special characters survive. Plain `%1` keeps those quotes and prints `"there"`, while `%~1` removes them.

In cmd, single quotes are not quotes, so `pux new hi 'echo hi'` arrives as separate words and fails. In PowerShell, single quotes work too.

To save a whole script, pipe it in or use the editor:

```bat
type setup.cmd | pux new setup
pux new deploy
```

A shebang works on Windows as well, as long as the interpreter is on your `PATH`. pux falls back to the bare name, so `#!/bin/bash` can find the `bash` from Git for Windows when it is on your `PATH`. Many Python installs only provide `python`, not `python3`, so use `#!/usr/bin/env python` there.

`pux --examples` and the editor hints show `%~1` on Windows, to match `cmd`.

## Finding commands

You rarely need to type a full name.

- **A unique start is enough.** `pux dep` runs `deploy` when nothing else starts with `dep`. If several names match, pux shows a list to pick from in a terminal. In scripts it prints the matches and exits with 1.
- **`pux ls dep`** lists the names that start with `dep`.
- **`pux find prod`** searches for `prod` anywhere in a name, so it finds `prod-db`, `deploy-prod` and `reprod-bug`. Keep typing to narrow the list, use the Up and Down keys to choose, and press Enter to run.

`cat` accepts a partial name too, and so does `edit` when it opens the editor. Replacing a command with a quoted argument or a pipe needs the exact name, and `rm` always does, since changing or deleting something should never be a guess.

## Names

A name can use lowercase letters, digits, `.`, `_` and `-`. It must start with a letter or a digit, cannot end with a dot, and can be up to 64 characters long.

Some names are reserved:

- names of pux commands, like `ls` or `new`
- names Windows refuses for files, like `con`, `nul` or `com1`, even with an extension like `nul.txt`

In the editor, pux reshapes the name as you type, so `My Deploy` becomes `my-deploy`.

## Where pux keeps things

Each command is a plain text file, one file per name, in a single folder:

| System | Folder |
|---|---|
| macOS, Linux | `~/.config/pux` (or `$XDG_CONFIG_HOME/pux` when that is set) |
| Windows | `%AppData%\pux` |

Set `PUX_DIR` to use another folder, for example one inside your dotfiles repo. Symlinked files work too.

Run `pux where` to see the exact location of the pux binary and of your commands on your machine.

## Using pux in scripts

When its output is piped or redirected, pux drops the colours and layout:

- `pux ls` and `pux find` print one name per line.
- `pux cat` prints the saved command byte for byte.
- Commands that would open the editor or a list tell you what to use instead.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | done |
| 1 | name not found, ambiguous or already taken, or another error |
| 2 | wrong usage or a bad name |
| 127 | the interpreter in the first line is not installed |
| 130 | cancelled |
| other | your command's own exit code |

## Settings

| Variable | Effect |
|---|---|
| `PUX_DIR` | keep commands in another folder |
| `PUX_ASCII=1` | plain symbols, for old consoles without unicode fonts |
| `NO_COLOR=1` | turn colour off |

## Uninstall

1. Remove the binary the way you installed it: `brew uninstall pux`, `scoop uninstall pux`, or delete the file that `pux where` shows.
2. Delete your saved commands by removing the folder that `pux where` shows. Uninstalling with Homebrew or Scoop does not touch this folder, so your commands are still there if you reinstall.

Run `pux where` before step 1, since it needs pux to still be installed.

## Using pux with AI

The [pux skill](skills/pux/SKILL.md) teaches AI assistants to use pux carefully: checking existing names first, quoting safely, and asking before changing a command you already have. Always look over what an assistant saves.

- **Claude Code:** copy the `skills/pux` folder into `~/.claude/skills/`, then ask something like "save a pux command that cleans old Docker images".
- **Any other assistant:** paste the contents of `SKILL.md` into the chat before asking.

## Development

```sh
git clone https://github.com/kyukey/pux.git
cd pux
go test ./...
go run ./cmd/pux
```

The code is split into a small core and the parts that face the user:

- `internal/store` saves and finds commands.
- `internal/runner` runs them.
- `internal/ui` draws the output, the prompts and the editor.
- `internal/cli` connects them into the `pux` commands.

## Contributing

Issues are welcome, but pull requests are not accepted. See [CONTRIBUTING.md](CONTRIBUTING.md) for how to report a bug or suggest an idea, and [SECURITY.md](SECURITY.md) for reporting a security problem privately.

## License

pux is licensed under the Apache License 2.0. See [LICENSE](LICENSE) for the full text.
