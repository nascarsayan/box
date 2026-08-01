package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const releasesAPI = "https://api.github.com/repos/nascarsayan/box/releases/latest"

// assetName matches what the release workflow uploads.
func assetName() string { return fmt.Sprintf("box-%s-%s", runtime.GOOS, runtime.GOARCH) }

var cmdSelfUpdate = &command{
	name:    "self-update",
	summary: "replace this binary with the latest release",
	long: `Downloads the newest box release for this platform and replaces the running
binary in place, then refreshes the copy inside the sandbox.

box is a single static binary with no dependencies, so updating it is a download and
a rename -- it does not touch the sandbox, the configuration, or nix. To update the
*configuration*, pull in the config repo and run 'box apply'; to update the pinned
package versions, run 'nix flake update' there.`,
	run: func(e *Env, args []string) error {
		if _, err := e.parseCommonFlags(args); err != nil {
			return err
		}

		step(1, 3, "checking for a newer release")
		tag, url, err := latestAsset()
		if err != nil {
			return err
		}
		if tag == version {
			okay("already on " + version)
			return nil
		}
		note(fmt.Sprintf("%s -> %s", version, tag))

		step(2, 3, "downloading "+assetName())
		self, err := os.Executable()
		if err != nil {
			return err
		}
		self, _ = filepath.EvalSymlinks(self)

		// Beside the target, so the rename is atomic on the same filesystem.
		tmp := self + ".new"
		if err := download(url, tmp); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Chmod(tmp, 0o755); err != nil {
			os.Remove(tmp)
			return err
		}
		// Fine on Linux: the old inode stays alive for this process.
		if err := os.Rename(tmp, self); err != nil {
			os.Remove(tmp)
			return err
		}
		okay(self)

		step(3, 3, "refreshing the sandbox copy")
		if err := e.installSelf(); err != nil {
			note("could not refresh it: " + err.Error())
		}
		return nil
	},
}

func latestAsset() (tag, url string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "box-self-update")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", "", fmt.Errorf("no releases published yet")
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github api returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", "", err
	}
	want := assetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			return rel.TagName, a.URL, nil
		}
	}
	return "", "", fmt.Errorf("release %s has no asset named %s", rel.TagName, want)
}

func download(url, dst string) error {
	// The parent may be missing: <sandbox>/tmp is created by init, but a concurrent
	// `box remove` can take it away mid-run, and os.Create alone then fails with a
	// bare ENOENT that says nothing about why.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
