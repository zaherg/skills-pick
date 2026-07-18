# skills-pick

`skills-pick` is a small terminal picker and installer for agent skills. It
ships a curated catalog, lets you search and select skills interactively, and
uses the `skills` package through `npx` to install or update them.

## Why prebuilt binaries?

This was built to overcome skills cli missing ability to install skills from a catalog file.
It is hard to follow up every single skill I use or install, remembering where I get it, which one
is the correct one, and when did I do that ... etc.
Plus having all the skills at a global level makes it hard to the agents to fully load their description,
so this will provide me with a TUI that I can use to sellect/chose the one I want without the need to remember
their information every single time.

GitHub is the release mirror and the release-download surface:

<https://github.com/zaherg/skills-pick/releases>

## Install a release binary

1. Open the [GitHub Releases](https://github.com/zaherg/skills-pick/releases)
   page and choose a version.
2. Download the archive matching your operating system and CPU architecture.
3. Extract the `skills-pick` executable (on Windows, `skills-pick.exe`) and
   put it on your `PATH`.

Release assets use these stable names, where `VERSION` is the release version
without the leading `v` from the Git tag:

```text
skills-pick_VERSION_linux_amd64.tar.gz
skills-pick_VERSION_linux_arm64.tar.gz
skills-pick_VERSION_darwin_amd64.tar.gz
skills-pick_VERSION_darwin_arm64.tar.gz
skills-pick_VERSION_windows_amd64.zip
skills-pick_VERSION_windows_arm64.zip
checksums.txt
```

Verify a downloaded archive with the SHA-256 entries in `checksums.txt` before
installing it. The release workflow builds with CGO disabled, so these
archives do not require a C compiler or Go runtime on the target machine.

On Linux, for example:

```sh
tar -xzf skills-pick_VERSION_linux_amd64.tar.gz
sha256sum -c checksums.txt --ignore-missing
install -m 0755 skills-pick "$HOME/bin/skills-pick"
```

On macOS, use the `darwin_amd64` or `darwin_arm64` archive as appropriate and
verify it with `shasum -a 256` (or another SHA-256 tool) before copying the
executable to a directory on your `PATH`:

```sh
tar -xzf skills-pick_VERSION_darwin_arm64.tar.gz
shasum -a 256 skills-pick
mkdir -p "$HOME/bin"
install -m 0755 skills-pick "$HOME/bin/skills-pick"
```

On Windows, download the matching `windows_amd64` or `windows_arm64` `.zip`,
verify it with PowerShell's `Get-FileHash -Algorithm SHA256`, and extract
`skills-pick.exe` into a directory on `PATH`:

```powershell
Get-FileHash .\skills-pick_VERSION_windows_amd64.zip -Algorithm SHA256
New-Item -ItemType Directory -Force $HOME\bin | Out-Null
Expand-Archive .\skills-pick_VERSION_windows_amd64.zip $HOME\bin
```

After installation, confirm the executable and its embedded catalog without
invoking `npx`:

```sh
skills-pick --version
skills-pick list
```

Use `skills-pick.exe --version` and `skills-pick.exe list` in PowerShell when
the executable is not exposed through an alias.

## Build from source

Go 1.26.5 (or the version selected by `go.mod`) is required. From a checkout:

```sh
make install
```

This builds the current source and installs it as `~/bin/skills-pick`. The
install target creates `~/bin` when needed and atomically overwrites an
existing `~/bin/skills-pick`. Add `~/bin` to your `PATH` if it is not already
there.

## What the binary does

The binary embeds `catalog.json` at build time. With no arguments it starts an
interactive terminal UI; use `j`/`k` to move, `space` to select, `/` to filter,
`enter` to install, and `q` to quit. The picker scans these global directories:

```text
~/.agents/skills
~/.claude/skills
~/.config/opencode/skills
```

and these directories in the current project:

```text
.agents/skills
.claude/skills
.opencode/skills
```

Already-installed skills are hidden from the picker in every category.

Install and update operations invoke `npx --yes skills ... -a opencode`, so Node.js, npm's
`npx`, and network access to the npm registry and skill sources are required.
The interactive picker also requires a TTY; running it with redirected or
non-interactive stdin prints an error. Use `list` or direct install mode in
scripts instead.

## Catalogs and overrides

Catalog loading follows this precedence order:

1. The file passed with `--catalog`.
2. `~/.config/skills-pick/catalog.json`, when it exists.
3. The catalog embedded in the binary.

The embedded catalog is the safe default and travels with the executable. To
add entries to a user catalog, use `add`; without `--catalog`, it writes
`~/.config/skills-pick/catalog.json` (creating its parent directory):

```sh
skills-pick add zaherg/chorus consensus
skills-pick add owner/repository my-skill --category Tools --desc "Short description"
```

`add` accepts `-c`/`--category` and `-d`/`--desc` (including `--category=` and
`--desc=` forms). A missing category defaults to `Tools`. Passing
`--catalog /path/to/catalog.json` selects a custom catalog for the command and
for catalog writes.

## Commands and flags

```text
skills-pick                         Start the interactive picker
skills-pick interactive              Start the interactive picker explicitly
skills-pick list                     List catalog skills
skills-pick <skill> [<skill> ...]    Install named skills directly
skills-pick add <source> <skill> ... Add skills to the catalog
skills-pick update                   Run `npx --yes skills update`
skills-pick changelog                Show the changelog
skills-pick help                    Show command help
```

Global flags must appear before the command or skill names:

```sh
skills-pick --version
skills-pick --help
skills-pick --filter seo
skills-pick --catalog ./catalog.json list
skills-pick --catalog ./catalog.json seo-audit ce-plan
```

`--filter` pre-applies a case-insensitive name filter in interactive mode.
`--version` prints the release version (or `dev` for a locally built binary)
and exits; `--help` prints the usage text and exits. Unknown direct-install
names are reported and cause a non-zero exit.

## Installation behavior and failures

Direct installs and selections from the picker are grouped by catalog source.
For each source, `skills-pick` runs one command equivalent to:

```sh
npx --yes skills add <source> -a opencode -y --skill <name> [--skill <name> ...]
```

The current implementation reports a failure for an individual source and
continues processing other sources. It then prints a final “Done. Installed”
summary for the requested selections, even if one source command failed;
check the per-source output and the installer state when diagnosing a partial
install.

## Release workflow

Pushing a tag in the form `vMAJOR.MINOR.PATCH` starts the release workflow. It
validates formatting, vet, tests, a normal build, and a GoReleaser snapshot;
checks that the canonical Gitea tag and GitHub mirror tag point to the same
commit; then publishes the archives and `checksums.txt` to the matching GitHub
release. Create the tag from a commit that has already passed the GitHub mirror's
branch CI run; the workflow refuses to publish when that exact-commit proof is
missing. It also refuses to overwrite an existing release for that tag.
