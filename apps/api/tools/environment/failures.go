package main

import "fmt"

// inOrder runs the steps one after the other and stops at the first that
// fails.
func inOrder(steps ...func() error) error {
	if len(steps) == 0 {
		return nil
	}
	if err := steps[0](); err != nil {
		return err
	}
	return inOrder(steps[1:]...)
}

// failedTo names what failed, and is nil when nothing did.
func failedTo(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", what, err)
}
