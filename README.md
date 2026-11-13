# box

> **Retired (2026-11-13).** box is no longer maintained and this repository is archived.
> Hosts use [`init-sh`](https://github.com/nascarsayan/init-sh); the shell templates live in
> [`box-config`](https://github.com/nascarsayan/box-config). Existing installs can run
> `box remove --yes --purge-config`.

A dev sandbox that needs no root: `nix-user-chroot` + `home-manager`, driven by one
static binary.

```sh
curl -fsSL https://snas.s.gy/box | sh -
```

Then `box` to enter it, or just `,`.

```sh
# somewhere other than the defaults
curl -fsSL https://snas.s.gy/box | sh -s -- -d /scratch/sandbox -c ~/mybox

# install the binary under a different name
curl -fsSL https://snas.s.gy/box | sh -s -- -n sbox

# just the binary, set nothing up yet
curl -fsSL https://snas.s.gy/box | sh -s -- --no-init
```

`-fsSL` matters: `snas.s.gy` is a 302, and without `-L` curl hands `sh` the
redirect body instead of the script.

Nothing is installed system-wide. No sudo, no package manager, no `/nix` on the
host. Everything lives under one directory you choose and can delete in one
command.

## What it is

`box` is only the front end. The environment itself — every package, dotfile, alias
and tool setting — is a nix configuration in a separate repo,
[nascarsayan/box-config](https://github.com/nascarsayan/box-config), cloned to
`~/.config/box`. `box` applies it.

That split is deliberate:

- **box** is a binary you never build. `box update` replaces it with the latest release, pulls the config, and rebuilds.
- **box-config** is a git clone you own. Your machine-specific values are
  gitignored, so any change you make to the shared library shows up as an ordinary
  `git diff` and can be sent upstream.

```
box                    the CLI (this repo, binary only)
~/.config/box          the configuration (box-config, cloned)
  modules/*.nix        tracked   -> the contributable library
  local.json           gitignored -> your paths and module selection
~/sandbox              the nix store, and $HOME while inside
```

## Commands

```
lifecycle
  init         create the sandbox: nix, the config clone, then apply
  apply        rebuild after changing the configuration
  update       update box and the configuration, then rebuild
  remove       delete the sandbox and its nix store

using it
  shell        enter the sandbox (bare `box`, and what `,` runs)
  run -- CMD   run one command inside it
  tmux         attach to the sandbox tmux session, creating it if needed

configuration
  modules      browse and toggle optional modules
  config       show this machine's resolved configuration

review workflow
  gwq          fetch a branch or PR into a worktree, print its path
  review       open a pull request in VS Code
  gc           delete worktrees and branches whose work has landed

other
  doctor       check this host can run a sandbox
```

`box help <command>` for the full description of any of them.

## Modules

Not everything is installed. `box modules` lists what is available with
descriptions and sizes, and toggles it:

```sh
box modules                     # picker
box modules enable kubernetes
box modules list
```

box has no built-in list. Each module is a file in box-config declaring its own
metadata, and box reads them with `nix eval .#moduleMeta` — which costs
milliseconds, because a module's `meta` never touches `pkgs`, so nixpkgs is not
instantiated just to print a list. Adding a module upstream needs no new version of
box.

## Requirements

Unprivileged user namespaces. That is the one hard requirement and the only one
without a fallback — it is what lets an ordinary account have a `/nix` at all.

```sh
box doctor
```

If they are disabled, a host admin needs to run
`sysctl -w user.max_user_namespaces=15000`.

## Notes

**A disabled module can still appear installed.** The host `PATH` is appended after
the sandbox's, on purpose, so host-only tools keep working — notably VS Code's
remote CLI, which `box review` shells out to. So `command -v kubectl` may resolve to
`/usr/bin/kubectl` with the `kubernetes` module off. `command -v` shows you which.

**box always applies from the local clone, never a `github:` ref.** Only `path:`
flakes see untracked files, and `local.json` is gitignored so the tracked tree stays
a clean library. Through a git ref the same configuration would silently evaluate
with its built-in defaults instead.

**Removing needs a command, not an `rm`.** Every path in a nix store is read-only,
so `rm -rf` fails on the first one. `box remove` makes the store writable first.

## Building from source

```sh
CGO_ENABLED=0 go build -o box .
```

Static on purpose: the same binary has to run outside the sandbox (to create and
delete it) and inside (where the store is mounted). A binary in the nix store could
not do the former.
