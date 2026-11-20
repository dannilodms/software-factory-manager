package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// checkout is the checkout of the repository a command runs in, and the
// main checkout it belongs to. They are the same directory unless the
// command runs in a Git worktree.
type checkout struct {
	// root holds the committed files the command reads: the choices of the
	// environment and the cloud-init file.
	root string
	// main holds what never leaves the machine: the .env with the token of
	// the provider and the local copy of the deploy key.
	main string
}

// envFile is the .env of the main checkout.
func (c checkout) envFile() string {
	return filepath.Join(c.main, ".env")
}

// findCheckout looks for the checkout that holds the directory, going up
// until it meets a `.git`.
func findCheckout(directory string) (checkout, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return checkout{}, err
	}
	return findCheckoutFrom(absolute, absolute)
}

func findCheckoutFrom(start, directory string) (checkout, error) {
	info, err := os.Stat(filepath.Join(directory, ".git"))
	if err == nil {
		return checkoutAt(directory, info)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return checkout{}, err
	}
	parent := filepath.Dir(directory)
	if parent == directory {
		return checkout{}, fmt.Errorf("%s is not inside a checkout of the repository", start)
	}
	return findCheckoutFrom(start, parent)
}

// checkoutAt reads the `.git` of a checkout. In the main checkout it is a
// directory. In a worktree it is a file that points at the worktree's
// directory inside the main checkout's `.git`, whose `commondir` file points
// back at that `.git`.
func checkoutAt(root string, git fs.FileInfo) (checkout, error) {
	if git.IsDir() {
		return checkout{root: root, main: root}, nil
	}
	pointer, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil {
		return checkout{}, err
	}
	gitDirectory, found := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir:")
	if !found {
		return checkout{}, fmt.Errorf("%s: no gitdir in it", filepath.Join(root, ".git"))
	}
	worktree := resolve(root, strings.TrimSpace(gitDirectory))
	common, err := os.ReadFile(filepath.Join(worktree, "commondir"))
	if err != nil {
		return checkout{}, fmt.Errorf("find the main checkout of the worktree %s: %w", root, err)
	}
	commonDirectory := resolve(worktree, strings.TrimSpace(string(common)))
	if filepath.Base(commonDirectory) != ".git" {
		return checkout{}, fmt.Errorf("find the main checkout of the worktree %s: its repository, %s, is not the .git of a checkout", root, commonDirectory)
	}
	return checkout{root: root, main: filepath.Dir(commonDirectory)}, nil
}

// resolve reads a path that Git wrote, relative to base unless absolute.
func resolve(base, path string) string {
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
