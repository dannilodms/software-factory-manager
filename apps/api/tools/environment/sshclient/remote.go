package sshclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Remote is a server that a publish works on, logged in to as its deploy
// user: it writes files there and runs commands. No command carries a
// value: a value travels as the content of a file, on the input of the
// command that writes it. What a command prints goes to the output.
type Remote struct {
	client *ssh.Client
	output io.Writer
}

// Open logs in to the target, trusting only its host key. What the commands
// print later goes to the output.
func Open(ctx context.Context, target Target, output io.Writer) (*Remote, error) {
	client, err := Dial(ctx, target)
	if err != nil {
		return nil, err
	}
	return &Remote{client: client, output: &lockedWriter{writer: output}}, nil
}

// WriteFile writes the file at that path, relative to the deploy user's
// home, in a folder that only the deploy user reads, which it creates when
// there is none. The content is written whole under another name and given
// its mode before it takes the place of the file, so the file is never seen
// half-written or with another mode.
func (r *Remote) WriteFile(ctx context.Context, name string, content []byte, mode fs.FileMode) error {
	temporary := name + ".new"
	command := fmt.Sprintf("umask 077 && mkdir -p %s && cat > %s && chmod %04o %s && mv -f %s %s",
		quoted(path.Dir(name)), quoted(temporary), mode.Perm(), quoted(temporary), quoted(temporary), quoted(name))
	return r.run(ctx, command, bytes.NewReader(content))
}

// Run runs the command and fails when it exits with a status other than 0.
func (r *Remote) Run(ctx context.Context, command string) error {
	return r.run(ctx, command, nil)
}

// Close logs out.
func (r *Remote) Close() error {
	return r.client.Close()
}

func (r *Remote) run(ctx context.Context, command string, input io.Reader) error {
	status, err := runWith(ctx, r.client, command, streams{input: input, output: r.output, errorOutput: r.output})
	if err != nil {
		return err
	}
	if status != 0 {
		return fmt.Errorf("%q exited with status %d", command, status)
	}
	return nil
}

// quoted writes a path as one word of the shell.
func quoted(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// lockedWriter takes what a command prints on its output and on its error
// output, which reach it at the same time.
type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (l *lockedWriter) Write(printed []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writer.Write(printed)
}
