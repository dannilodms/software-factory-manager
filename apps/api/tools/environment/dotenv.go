package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// A `.env` is read as Task reads it: `NAME=value` on a line, `export ` before
// it allowed, `#` starting a comment, and the value in quotes or not.

// valueIn reads the value of one variable from a `.env`. The last line that
// sets the name wins. A file that does not exist sets nothing.
func valueIn(path, name string) (string, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	value := ""
	for line := range strings.Lines(string(content)) {
		value = assignedIn(line, name, value)
	}
	return value, nil
}

// appendTo writes lines at the end of a `.env`, in the line endings the file
// uses, and leaves every line before them as it was. A file that does not
// exist is created first, empty and reached by its owner alone: on Windows,
// where a mode of Go sets no access list, with an access list of its own.
// One that exists keeps the access it has.
func appendTo(path, lines string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		err = writePrivate(path, nil)
	}
	if err != nil {
		return err
	}
	newline := "\n"
	if bytes.Contains(content, []byte("\r\n")) {
		newline = "\r\n"
	}
	lines = strings.ReplaceAll(lines, "\n", newline)
	if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
		lines = newline + lines
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return err
	}
	_, err = file.WriteString(lines)
	return errors.Join(err, file.Close())
}

// namesIn gives the names a `.env` sets, in the order it sets them, each
// once.
func namesIn(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var names []string
	for line := range strings.Lines(string(content)) {
		names = withNameOf(names, line)
	}
	return names, nil
}

// assignedIn gives the value the line sets for the name, or the one before
// it when the line sets another name or none.
func assignedIn(line, name, before string) string {
	set, value, sets := assignment(line)
	if !sets || set != name {
		return before
	}
	return value
}

// withNameOf adds the name the line sets, when it sets one not there yet.
func withNameOf(names []string, line string) []string {
	name, _, sets := assignment(line)
	if !sets || slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

// assignment reads the name a line sets and its value. A comment, a blank
// line and a line with no `=` set nothing.
func assignment(line string) (name, value string, sets bool) {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		return "", "", false
	}
	name, value, sets = strings.Cut(strings.TrimPrefix(line, "export "), "=")
	name = strings.TrimSpace(name)
	return name, unquoted(strings.TrimSpace(value)), sets && name != ""
}

// unquoted takes the value out of its quotes, or a comment off a value with
// none.
func unquoted(value string) string {
	if value == "" {
		return ""
	}
	quote := value[:1]
	if quote != `"` && quote != `'` {
		uncommented, _, _ := strings.Cut(value, " #")
		return strings.TrimSpace(uncommented)
	}
	inside, _, _ := strings.Cut(value[1:], quote)
	return inside
}
