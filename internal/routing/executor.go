package routing

import "os/exec"

// Executor abstracts command execution for ipset/dnsmasq/iptables operations.
type Executor interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type osExec struct{}

func (osExec) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (osExec) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
