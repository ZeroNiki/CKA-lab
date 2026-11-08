package cluster

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"

	"cka-lab/internal/check"
)

// SSHHost is one entry of `vagrant ssh-config` output.
type SSHHost struct {
	Host, HostName, User, IdentityFile string
	Port                               int
}

// ParseSSHConfig reads the subset of ssh_config that `vagrant ssh-config` produces.
func ParseSSHConfig(r io.Reader) (map[string]SSHHost, error) {
	hosts := map[string]SSHHost{}
	var cur *SSHHost
	flush := func() {
		if cur != nil {
			hosts[cur.Host] = *cur
		}
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch strings.ToLower(key) {
		case "host":
			flush()
			cur = &SSHHost{Host: value, Port: 22}
		case "hostname":
			if cur != nil {
				cur.HostName = value
			}
		case "user":
			if cur != nil {
				cur.User = value
			}
		case "port":
			if cur != nil {
				p, err := strconv.Atoi(value)
				if err != nil {
					return nil, fmt.Errorf("host %s: bad port %q", cur.Host, value)
				}
				cur.Port = p
			}
		case "identityfile":
			if cur != nil {
				cur.IdentityFile = value
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()
	if len(hosts) == 0 {
		return nil, errors.New("ssh config has no hosts (run ./lab up)")
	}
	return hosts, nil
}

// SSHExecutor runs scripts on lab nodes. It implements check.NodeExecutor.
type SSHExecutor struct {
	hosts map[string]SSHHost
}

func NewSSHExecutor(sshConfigPath string) (*SSHExecutor, error) {
	f, err := os.Open(sshConfigPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hosts, err := ParseSSHConfig(f)
	if err != nil {
		return nil, err
	}
	return &SSHExecutor{hosts: hosts}, nil
}

func (e *SSHExecutor) Run(ctx context.Context, node, script string) (string, int, error) {
	h, ok := e.hosts[node]
	if !ok {
		return "", -1, fmt.Errorf("unknown node %q", node)
	}
	key, err := os.ReadFile(h.IdentityFile)
	if err != nil {
		return "", -1, fmt.Errorf("%w: read key for %s: %v", check.ErrUnavailable, node, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return "", -1, err
	}
	cfg := &ssh.ClientConfig{
		User: h.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// Lab VMs get new host keys whenever they are recreated, and live on a private libvirt network.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(h.HostName, strconv.Itoa(h.Port)))
	if err != nil {
		return "", -1, fmt.Errorf("%w: %v", check.ErrUnavailable, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, h.HostName, cfg)
	if err != nil {
		conn.Close()
		return "", -1, fmt.Errorf("%w: ssh %s: %v", check.ErrUnavailable, node, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()
	// Close the connection when ctx ends so a hung command cannot block a check forever.
	stop := context.AfterFunc(ctx, func() { client.Close() })
	defer stop()

	sess, err := client.NewSession()
	if err != nil {
		return "", -1, fmt.Errorf("%w: %v", check.ErrUnavailable, err)
	}
	defer sess.Close()
	var stdout bytes.Buffer
	sess.Stdout = &stdout
	sess.Stdin = strings.NewReader(script)
	err = sess.Run("bash -s")
	var exitErr *ssh.ExitError
	switch {
	case err == nil:
		return stdout.String(), 0, nil
	case errors.As(err, &exitErr):
		return stdout.String(), exitErr.ExitStatus(), nil
	case ctx.Err() != nil:
		return stdout.String(), -1, fmt.Errorf("%w: %s: %v", check.ErrUnavailable, node, ctx.Err())
	default:
		return stdout.String(), -1, fmt.Errorf("%w: %v", check.ErrUnavailable, err)
	}
}
