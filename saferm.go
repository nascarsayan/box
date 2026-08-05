package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// mountsUnder returns every mount point at or beneath dir, per this process's
// mount namespace.
//
// Namespace matters: nix-user-chroot's binds are visible from some shells and not
// others, so the same delete is harmless in one and catastrophic in another. Read
// mountinfo at the moment of deletion rather than trusting an earlier look.
func mountsUnder(dir string) ([]string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	clean := filepath.Clean(dir)
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		// mountinfo field 5 is the mount point.
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 {
			continue
		}
		mp := fields[4]
		if mp == clean || strings.HasPrefix(mp, clean+string(os.PathSeparator)) {
			out = append(out, mp)
		}
	}
	return out, sc.Err()
}

// safeRemoveAll deletes dir, refusing to cross a filesystem boundary.
//
// The reason this is not os.RemoveAll: nix-user-chroot bind-mounts the entire host
// root inside its scratch directory. A plain recursive delete that meets one of
// those binds walks straight out of the sandbox and starts unlinking /dev, /etc
// and /usr as root. That is not hypothetical -- it stripped every device node from
// a live machine, sparing only the mount points, which fail with EBUSY.
//
// Two guards: refuse up front if anything is mounted beneath dir, and compare
// st_dev on every directory as we descend so a mount appearing mid-delete is still
// caught.
func safeRemoveAll(dir string) error {
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil
	}

	if mounts, err := mountsUnder(dir); err == nil && len(mounts) > 0 {
		return fmt.Errorf(
			"refusing to delete %s: %d filesystem(s) are mounted beneath it\n"+
				"       %s\n"+
				"       These are bind mounts of the host, so deleting through them would\n"+
				"       destroy the host. Exit any running sandbox shells first:\n"+
				"         pkill -f nix-user-chroot",
			dir, len(mounts), strings.Join(mounts, "\n       "))
	}

	var rootDev uint64
	var st syscall.Stat_t
	if err := syscall.Lstat(dir, &st); err != nil {
		return err
	}
	rootDev = uint64(st.Dev)

	// Depth-first, so children go before their parent.
	var walk func(string) error
	walk = func(p string) error {
		var s syscall.Stat_t
		if err := syscall.Lstat(p, &s); err != nil {
			return nil // vanished under us; nothing to do
		}
		// Symlinks are unlinked, never followed.
		if s.Mode&syscall.S_IFMT == syscall.S_IFLNK {
			return os.Remove(p)
		}
		if s.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			return os.Remove(p)
		}
		if uint64(s.Dev) != rootDev {
			return fmt.Errorf("refusing to descend into %s: it is on a different "+
				"filesystem (a mount appeared during the delete)", p)
		}
		// The nix store is read-only; parents need +w before entries can go.
		_ = os.Chmod(p, os.FileMode(s.Mode&0o7777)|0o300)

		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		for _, ent := range entries {
			if err := walk(filepath.Join(p, ent.Name())); err != nil {
				return err
			}
		}
		return os.Remove(p)
	}
	return walk(dir)
}
