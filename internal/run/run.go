package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const timeout = 15 * time.Second

// Output runs a command and returns trimmed stdout, folding stderr into the error.
func Output(ctx context.Context, name string, args ...string) (string, error) {
	out, err := Bytes(ctx, nil, name, args...)
	return strings.TrimSpace(string(out)), err
}

func Bytes(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	return Exec(ctx, timeout, stdin, name, args...)
}

func Exec(ctx context.Context, limit time.Duration, stdin []byte, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var missing *exec.Error
	if errors.As(err, &missing) {
		return nil, fmt.Errorf("%s is not installed", name)
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return nil, fmt.Errorf("%s: %w", name, err)
}

func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
