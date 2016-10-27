package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/NetSys/quilt/util"

	log "github.com/Sirupsen/logrus"
	homedir "github.com/mitchellh/go-homedir"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"
)

// NativeClient is wrapper over Go's SSH client.
type NativeClient struct {
	session *ssh.Session
	pty     *pty
}

// NewNativeClient creates a new NativeClient.
func NewNativeClient() *NativeClient {
	return &NativeClient{}
}

// Connect establishes an SSH session with reasonable, quilt-specific defaults.
func (c *NativeClient) Connect(host string, keyPath string) error {
	var auth ssh.AuthMethod
	if keyPath != "" {
		signer, err := signerFromFile(keyPath)
		if err != nil {
			return err
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.PublicKeys(defaultSigners()...)
	}
	sshConfig := &ssh.ClientConfig{
		User: "quilt",
		Auth: []ssh.AuthMethod{auth},
	}

	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", host), sshConfig)
	if err != nil {
		return err
	}

	c.session, err = conn.NewSession()
	if err != nil {
		return err
	}
	return nil
}

var defaultKeys = []string{"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"}

// Gets the signers for the default private key locations if possible
func defaultSigners() []ssh.Signer {
	var signers []ssh.Signer
	if sa, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		agentSigners, err := agent.NewClient(sa).Signers()
		if err != nil {
			log.Warning("Error getting keys from ssh-agent")
		} else {
			signers = agentSigners
		}
	}

	dir, err := homedir.Dir()
	if err != nil {
		log.WithError(err).Warn("Error getting path of home directory")
		return signers
	}

	sshDir := filepath.Join(dir, ".ssh")
	for _, keyName := range defaultKeys {
		identityPath := filepath.Join(sshDir, keyName)
		key, err := signerFromFile(identityPath)
		if err != nil {
			log.WithError(err).WithField("path", identityPath).
				Debug("Unable to load default identity file")
			continue
		}
		signers = append(signers, key)
	}
	return signers
}

// RequestPTY requests a pseudo-terminal on the remote server.
func (c *NativeClient) RequestPTY() error {
	if c.session == nil {
		return errors.New("no open SSH session")
	}

	c.pty = newPty(c.session)
	return c.pty.Request()
}

// Run runs an SSH command, erroring if no connection is open.
func (c *NativeClient) Run(command string) error {
	if c.session == nil {
		return errors.New("no open SSH session")
	}
	c.session.Stdout = os.Stdout
	c.session.Stdin = os.Stdin
	c.session.Stderr = os.Stderr

	return c.session.Run(command)
}

// Disconnect closes SSH session, erroring if none open.
func (c *NativeClient) Disconnect() error {
	if c.session == nil {
		return errors.New("no open SSH session")
	}

	if c.pty != nil {
		if err := c.pty.Close(); err != nil {
			return err
		}
	}

	return c.session.Close()
}

// pty encapsulates pseudo-terminal operations.
type pty struct {
	session        *ssh.Session
	fileDescriptor int
	originalState  *terminal.State
	resizeSignal   chan os.Signal
	height         int
	width          int
}

func newPty(session *ssh.Session) *pty {
	return &pty{
		session:      session,
		resizeSignal: make(chan os.Signal, 1),
	}
}

// Request requests a PTY with opinionated defaults.
func (p *pty) Request() error {
	p.fileDescriptor = int(os.Stdin.Fd())
	if !terminal.IsTerminal(p.fileDescriptor) {
		return errors.New("TTY should be requested from a terminal")
	}

	var err error
	p.originalState, err = terminal.MakeRaw(p.fileDescriptor)
	if err != nil {
		return err
	}

	p.width, p.height, err = terminal.GetSize(p.fileDescriptor)
	if err != nil {
		return err
	}

	err = p.session.RequestPty("xterm", p.width, p.height, ssh.TerminalModes{
		ssh.ECHO:          1,     // display what's being typed
		ssh.ECHOCTL:       1,     // allow ^CONTROL_CHARACTERS
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	})
	if err != nil {
		return err
	}

	signal.Notify(p.resizeSignal, syscall.SIGWINCH)
	go p.monitorWindowSize()
	return nil
}

// Close tears down the TTY, restoring the terminal to its original state.
func (p *pty) Close() error {
	signal.Stop(p.resizeSignal)
	return terminal.Restore(p.fileDescriptor, p.originalState)
}

// windowChange is the payload for the request that tells the remote SSH server
// to adjust the dimensions of future message bodies.
// Source: https://www.ietf.org/rfc/rfc4254.txt 6.7: Window Dimension Change Message
type windowChange struct {
	columns  uint32
	rows     uint32
	widthPx  uint32
	heightPx uint32
}

func (p *pty) monitorWindowSize() {
	for range p.resizeSignal {
		width, height, err := terminal.GetSize(p.fileDescriptor)
		if err != nil {
			log.WithError(err).Warn("Error getting terminal window size")
			continue
		}

		if p.width == width && p.height == height {
			continue
		}

		p.width = width
		p.height = height

		payload := ssh.Marshal(windowChange{
			columns:  uint32(width),
			rows:     uint32(height),
			widthPx:  0,
			heightPx: 0,
		})
		_, err = p.session.SendRequest("window-change", false, payload)
		if err != nil {
			log.WithError(err).Warn("Error adjusting terminal window size")
		}
	}
}

func signerFromFile(file string) (ssh.Signer, error) {
	fileStr, err := util.ReadFile(file)
	if err != nil {
		return nil, err
	}

	key, err := ssh.ParsePrivateKey([]byte(fileStr))
	if err != nil {
		return nil, err
	}
	return key, nil
}
