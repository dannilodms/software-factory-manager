// Package sshclienttest gives a test an SSH server in its own process, on
// the loopback interface. It answers with the host key the test gives it,
// lets in one user with one key, and answers each command with the output
// and the status the test sets, recording every command and what it read on
// its input.
package sshclienttest

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"slices"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
)

// Command is what the server was asked to run.
type Command struct {
	Line  string
	Input []byte
}

// Server is an SSH server that lives until the test ends.
type Server struct {
	// Address is where it listens, as host:port.
	Address string

	user       string
	authorized ssh.PublicKey
	config     *ssh.ServerConfig
	listener   net.Listener

	mu       sync.Mutex
	statuses map[string]uint32
	outputs  map[string]output
	commands []Command
	offered  int
	refusals int
}

// NewServer starts a server with that host key, which lets in the user
// with the authorized key.
func NewServer(t testing.TB, hostKey ssh.Signer, user string, authorized ssh.PublicKey) *Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for SSH: %v", err)
	}
	server := &Server{
		Address:    listener.Addr().String(),
		user:       user,
		authorized: authorized,
		listener:   listener,
		statuses:   map[string]uint32{},
		outputs:    map[string]output{},
	}
	server.config = &ssh.ServerConfig{PublicKeyCallback: server.login}
	server.config.AddHostKey(hostKey)
	t.Cleanup(func() { listener.Close() })
	go server.accept()
	return server
}

// Answer makes the command exit with that status from now on. A command
// with no answer exits 0.
func (s *Server) Answer(command string, status uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[command] = status
}

// output is what a command prints.
type output struct{ stdout, stderr string }

// Print makes the command print that on its output and its error output
// from now on. A command with nothing to print prints nothing.
func (s *Server) Print(command, stdout, stderr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outputs[command] = output{stdout: stdout, stderr: stderr}
}

// RefuseLogins makes the next logins fail, as a server does before its
// user exists.
func (s *Server) RefuseLogins(count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refusals = count
}

// Commands gives what the server ran, in order.
func (s *Server) Commands() []Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.commands)
}

// Offered tells how many times a client offered a key to log in.
func (s *Server) Offered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offered
}

func (s *Server) login(metadata ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offered++
	if s.refusals > 0 {
		s.refusals--
		return nil, errRefused
	}
	if metadata.User() != s.user || !bytes.Equal(key.Marshal(), s.authorized.Marshal()) {
		return nil, errRefused
	}
	return &ssh.Permissions{}, nil
}

// accept takes clients until the listener is closed.
func (s *Server) accept() {
	for connection, err := s.listener.Accept(); err == nil; connection, err = s.listener.Accept() {
		go s.serve(connection)
	}
}

// serve answers one client.
func (s *Server) serve(connection net.Conn) {
	defer connection.Close()
	_, channels, requests, err := ssh.NewServerConn(connection, s.config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for channel := range channels {
		go s.session(channel)
	}
}

// session answers one session channel: one command and its status.
func (s *Server) session(request ssh.NewChannel) {
	if request.ChannelType() != "session" {
		_ = request.Reject(ssh.UnknownChannelType, "only sessions")
		return
	}
	channel, requests, err := request.Accept()
	if err != nil {
		return
	}
	defer channel.Close()
	for each := range requests {
		s.answer(channel, each)
	}
}

// answer runs an exec request and refuses any other: it reads the input to
// its end, prints and exits as the test said.
func (s *Server) answer(channel ssh.Channel, request *ssh.Request) {
	var exec struct{ Command string }
	if request.Type != "exec" || ssh.Unmarshal(request.Payload, &exec) != nil {
		_ = request.Reply(false, nil)
		return
	}
	_ = request.Reply(true, nil)
	input, _ := io.ReadAll(channel)
	s.mu.Lock()
	s.commands = append(s.commands, Command{Line: exec.Command, Input: input})
	status, printed := s.statuses[exec.Command], s.outputs[exec.Command]
	s.mu.Unlock()
	_, _ = io.WriteString(channel, printed.stdout)
	_, _ = io.WriteString(channel.Stderr(), printed.stderr)
	_, _ = channel.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, status))
	channel.Close()
}

type refusal string

func (r refusal) Error() string { return string(r) }

const errRefused = refusal("the key is not authorized")
