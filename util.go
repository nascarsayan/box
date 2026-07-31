package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime"
)

func isARM64() bool { return runtime.GOARCH == "arm64" }

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
