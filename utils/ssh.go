package utils

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"code.google.com/p/go.crypto/ssh"
	"code.google.com/p/go.crypto/ssh/agent"
	"code.google.com/p/go.crypto/ssh/terminal"
)

// return all keys known to the agent
// TODO: do we want to fetch pk files too?
// TODO: only create one agent if there are multiple connections
func pkSigners() []ssh.Signer {
	s, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK"))
	if err != nil {
		log.Println("Failed to connect to ssh-agent:", err)
		return nil
	}

	a := agent.NewClient(s)

	signers, err := a.Signers()
	if err != nil {
		log.Println(err)
		return nil
	}

	return signers
}

// ssh "window-change"
// rfc4254 section 6.7
type winCh struct {
	columns   uint32
	rows      uint32
	width_px  uint32
	height_px uint32
}

// Watch for changes in window size, and send resize requests to the remote pty
func watchWinSize(sess *ssh.Session, stop chan struct{}) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGWINCH)
	for {
		select {
		case <-c:
		case <-stop:
			signal.Stop(c)
			return
		}

		cols, rows, _ := terminal.GetSize(0)
		_, err := sess.SendRequest("window-change", false, ssh.Marshal(winCh{uint32(cols), uint32(rows), 0, 0}))
		if err != nil {
			log.Println("window-change error:", err)
		}
	}
}

// connect an ssh client, and return an active session
func clientSession(host string) (*ssh.Session, error) {

	username := "galaxy"
	if strings.Contains(host, "127.0.0.1:2222") {
		username = "vagrant"
	}

	port := "22"
	hostPort := strings.SplitN(host, ":", 2)
	if len(hostPort) > 1 {
		host, port = hostPort[0], hostPort[1]
	}

	var auths []ssh.AuthMethod
	auths = append(auths, ssh.PublicKeys(pkSigners()...))

	cfg := &ssh.ClientConfig{
		User: username,
		Auth: auths,
	}
	cfg.SetDefaults()

	fmt.Printf("Connecting to %s...\n", host+":"+port)
	client, err := ssh.Dial("tcp", host+":"+port, cfg)
	if err != nil {
		return nil, err
	}

	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	return sess, nil
}

// Run a command on a remote host
// TODO: background and debug don't do anything
func SSHCmd(host string, command string, background bool, debug bool) {
	sess, err := clientSession(host)
	if err != nil {
		log.Println(err)
		return
	}

	output, err := sess.CombinedOutput(command)
	if err != nil {
		log.Println(err)
	}

	_, err = os.Stdout.Write(output)
	if err != nil {
		log.Println(err)
	}
	log.Println("remote command complete")
}

// Establish an interactive shell on the remote host
func SSHShell(host string) {
	sess, err := clientSession(host)
	if err != nil {
		log.Println(err)
		return
	}

	si, _ := sess.StdinPipe()
	so, _ := sess.StdoutPipe()
	se, _ := sess.StderrPipe()

	// our own terminal needs to be put in raw mode
	oldState, err := terminal.MakeRaw(0)
	if err != nil {
		log.Println(err)
		return
	}
	defer terminal.Restore(0, oldState)

	cols, rows, _ := terminal.GetSize(0)

	err = sess.RequestPty(os.Getenv("TERM"), rows, cols, nil)
	if err != nil {
		log.Println(err)
		return
	}

	stopWatch := make(chan struct{})
	go watchWinSize(sess, stopWatch)
	defer close(stopWatch)

	err = sess.Shell()
	if err != nil {
		log.Println(err)
		return
	}

	go io.Copy(si, os.Stdin)
	go io.Copy(os.Stderr, se)
	io.Copy(os.Stdout, so)
}
