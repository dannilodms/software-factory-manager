package sshclient

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"
)

// PreparedSignal is the file that the cloud-init file writes last, once
// every step of it succeeded.
const PreparedSignal = "/var/lib/software-factory-manager/server-prepared"

const (
	// cloudInitWait blocks until cloud-init has finished, and exits 0 when it
	// finished cleanly, 1 when it failed and 2 when it finished with a
	// warning, such as a key of the provider's own configuration that
	// cloud-init deprecates.
	cloudInitWait = "cloud-init status --wait"
	// preparedCheck exits 0 when the server is prepared.
	preparedCheck = "test -f " + PreparedSignal
)

// Readiness waits until a new server answers SSH with the expected host key
// and cloud-init has prepared it.
type Readiness struct {
	// RetryInterval is the pause between two attempts to log in.
	RetryInterval time.Duration
}

// Wait tries to log in until it can, then waits for cloud-init. A server
// that cannot be reached, answers with another host key or refuses the
// deploy key is tried again until the context ends; a cloud-init that fails
// ends the wait at once.
func (r Readiness) Wait(ctx context.Context, target Target) error {
	var reason error
	done, err := r.try(ctx, target)
	for !done {
		reason = cmp.Or(err, reason)
		done, err = r.again(ctx, target, reason)
	}
	return err
}

// again pauses and tries once more. When the context ends first, it gives
// up with the reason of the last attempt that the end of the context did not
// cut short.
func (r Readiness) again(ctx context.Context, target Target, reason error) (bool, error) {
	select {
	case <-ctx.Done():
		return true, fmt.Errorf("the server at %s was not ready in time: %w", target.Address, cmp.Or(reason, ctx.Err()))
	case <-time.After(r.RetryInterval):
	}
	return r.try(ctx, target)
}

// try makes one attempt. It tells whether the wait is over, and why not:
// no reason when the end of the context cut the attempt short.
func (r Readiness) try(ctx context.Context, target Target) (bool, error) {
	client, err := Dial(ctx, target)
	if err != nil {
		return false, unlessCut(ctx, err)
	}
	defer client.Close()
	status, err := Run(ctx, client, cloudInitWait)
	if err != nil {
		return false, unlessCut(ctx, err)
	}
	if status != 0 && status != 2 {
		return true, fmt.Errorf("cloud-init failed on the server, with status %d: `cloud-init status --long` there tells why", status)
	}
	prepared, err := Run(ctx, client, preparedCheck)
	if err != nil {
		return false, unlessCut(ctx, err)
	}
	if prepared != 0 {
		return true, errors.New("cloud-init finished, but the cloud-init file did not run to its end: `cloud-init status --long` on the server tells why")
	}
	return true, nil
}

// unlessCut drops an error that the end of the context caused.
func unlessCut(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	return err
}
