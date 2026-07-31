package main

import "fmt"

// Thin wrappers over the shell helpers in box-config/pkgs. That logic stays shell:
// it is debugged against real merged, squashed, deleted-branch and cross-fork PRs.

var cmdGwq = &command{
	name:    "gwq",
	args:    "[-b] <branch | PR number> [base]",
	summary: "fetch a branch or PR into a worktree, print its path",
	long: `Prepares a branch for review and prints the worktree path on stdout, so the
shell can cd into it. Everything else goes to stderr.

  box gwq 784                  all digits: that PR's head branch, and the PR's
                               own base becomes the review base
  box gwq feature/thing        an existing branch, fetched from the remote
  box gwq -b feature/new       create a branch forked from a freshly fetched base
  box gwq 784 release-1.2      override the base

The ,gwq shell function wraps this to cd the calling shell; box cannot do that
itself, since a child process cannot change its parent's directory.

Handles the cases that break a naive fetch: a PR whose head branch was deleted
after merging, or which came from a fork, is fetched via refs/pull/<n>/head; a
branch gone from the remote but present locally degrades to the local one with a
warning; a diverged local branch is reported rather than reset.

Creating a worktree also reaps merged ones in the background -- see 'box help gc'.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		if len(rest) == 0 {
			return fmt.Errorf("nothing to review\n       usage: box gwq [-b] <branch | PR number> [base]")
		}
		path, err := e.dispatchCapture(append([]string{"box-gwq-review"}, rest...)...)
		if err != nil {
			return err
		}
		if path != "" {
			fmt.Println(path)
		}
		return nil
	},
}

var cmdReview = &command{
	name:    "review",
	args:    "[PR number]",
	summary: "open a pull request in VS Code",
	long: `Opens the PR's changed-files view through the GitHub Pull Requests extension.

With no argument it resolves the PR for the current branch. Needs the VS Code
remote CLI on PATH, which the host provides under ~/.vscode-server -- box appends
the host PATH after the sandbox profile's precisely so host tools like this
still resolve.`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		return e.dispatch(append([]string{"box-review-open"}, rest...)...)
	},
}

var cmdGC = &command{
	name:    "gc",
	args:    "[--dry-run] [--skip PATH]",
	summary: "delete worktrees and branches whose work has landed",
	long: `Removes gwq worktrees whose pull request is merged, and their local branches.

Deliberately conservative, because it also runs unattended after every new
worktree: anything with uncommitted or untracked content is skipped, as are the
base branch, the main worktree, whatever the main checkout has checked out,
detached worktrees, and any path outside gwq's basedir.

Merge detection asks the PR's state first, so a squash-merged branch -- whose
commits are no longer ancestors of the base -- is still recognised as landed.

  box gc --dry-run    show what would go, change nothing`,
	run: func(e *Env, args []string) error {
		rest, err := e.parseCommonFlags(args)
		if err != nil {
			return err
		}
		return e.dispatch(append([]string{"box-gwq-gc"}, rest...)...)
	},
}

var cmdTmux = &command{
	name:    "tmux",
	summary: "attach to the sandbox tmux session, creating it if needed",
	long: `Attaches to the session named by tmuxSocket in local.json, starting it if it is
not running. This is what ,, runs.

The sandbox sets TMUX_TMPDIR to <sandbox>/run, so its tmux server is separate from
any server started outside the sandbox. Two sandboxes, or a sandbox and the host,
never fight over one socket -- and ,, in one does not attach to the other.`,
	run: func(e *Env, args []string) error {
		if _, err := e.parseCommonFlags(args); err != nil {
			return err
		}
		conf, err := e.readLocalConf()
		socket := e.Username
		if err == nil && conf.TmuxSocket != "" {
			socket = conf.TmuxSocket
		}
		return e.dispatch("tmux", "-L", socket, "new-session", "-A", "-s", socket)
	},
}
