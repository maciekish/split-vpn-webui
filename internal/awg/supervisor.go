package awg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unicode"
)

// Supervisor owns the lifecycle of one tunnel process: backend selection,
// bring-up, hook execution, systemd readiness notification, and teardown on
// SIGTERM/SIGINT or device death.
type Supervisor struct {
	Spec *TunnelSpec
	Deps *BackendDeps
}

// Run blocks until the tunnel is stopped or dies. A nil error means a clean,
// requested shutdown.
func (s *Supervisor) Run(ctx context.Context) error {
	backend, err := SelectBackend(ctx, s.Spec, s.Deps)
	if err != nil {
		return err
	}
	s.Deps.logf("bringing up %s via %s engine", s.Spec.InterfaceName, backend.Name())

	if err := s.runHooks(ctx, "PreUp", s.Spec.PreUp); err != nil {
		return err
	}

	if err := backend.Up(ctx, s.Spec); err != nil {
		// A kernel module that loads but misbehaves must not strand the
		// tunnel; retry through the userspace engine when the parameter set
		// allows it.
		if backend.Name() == "kernel" && !s.Spec.Params.UsesExtendedPadding() {
			s.Deps.logf("kernel engine failed (%v); falling back to userspace engine", err)
			backend = newUserspaceBackend(s.Deps)
			if err := backend.Up(ctx, s.Spec); err != nil {
				return fmt.Errorf("userspace fallback failed: %v", err)
			}
		} else {
			return err
		}
	}

	if err := s.runHooks(ctx, "PostUp", s.Spec.PostUp); err != nil {
		_ = backend.Down(ctx, s.Spec)
		return err
	}

	sdNotify("READY=1")
	s.Deps.logf("%s is up (engine: %s)", s.Spec.InterfaceName, backend.Name())

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)

	died := backend.Dead()
	var runErr error
	select {
	case sig := <-signals:
		s.Deps.logf("received %s, shutting down %s", sig, s.Spec.InterfaceName)
	case <-died:
		runErr = fmt.Errorf("tunnel device %s terminated unexpectedly", s.Spec.InterfaceName)
	case <-ctx.Done():
		runErr = ctx.Err()
	}

	sdNotify("STOPPING=1")
	// Teardown must proceed even if the start context was cancelled.
	teardownCtx := context.WithoutCancel(ctx)
	if err := s.runHooks(teardownCtx, "PreDown", s.Spec.PreDown); err != nil {
		s.Deps.logf("warning: %v", err)
	}
	if err := backend.Down(teardownCtx, s.Spec); err != nil {
		s.Deps.logf("warning: teardown of %s: %v", s.Spec.InterfaceName, err)
	}
	if err := s.runHooks(teardownCtx, "PostDown", s.Spec.PostDown); err != nil {
		s.Deps.logf("warning: %v", err)
	}
	return runErr
}

// runHooks executes admin-authored hook commands with %i replaced by the
// interface name. Shell metacharacters are treated as plain arguments; this
// keeps execution on explicit argv slices instead of `/bin/sh -c`.
func (s *Supervisor) runHooks(ctx context.Context, label string, hooks []string) error {
	for _, hook := range hooks {
		command := strings.ReplaceAll(hook, "%i", s.Spec.InterfaceName)
		argv, err := splitHookCommand(command)
		if err != nil {
			return fmt.Errorf("%s hook %q is invalid: %v", label, command, err)
		}
		s.Deps.logf("running %s hook: %s", label, command)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		output, err := cmd.CombinedOutput()
		if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
			s.Deps.logf("%s output: %s", label, trimmed)
		}
		if err != nil {
			return fmt.Errorf("%s hook failed (%s): %v", label, command, err)
		}
	}
	return nil
}

func splitHookCommand(command string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	inToken := false

	flush := func() {
		if !inToken {
			return
		}
		args = append(args, b.String())
		b.Reset()
		inToken = false
	}

	for _, r := range command {
		if escaped {
			b.WriteRune(r)
			escaped = false
			inToken = true
			continue
		}
		if r == '\\' {
			escaped = true
			inToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
			inToken = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			inToken = true
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		b.WriteRune(r)
		inToken = true
	}
	if escaped {
		return nil, fmt.Errorf("unterminated escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	if len(args) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return args, nil
}
