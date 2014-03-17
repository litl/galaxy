package main

import (
	"bytes"
	"code.google.com/p/go.crypto/ssh"
	"fmt"
	"github.com/wsxiaoys/terminal/color"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func findPrivateKeys(root string) []string {
	var availableKeys = []string{}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Skip really big files to avoid OOM errors since they are
		// unlikely to be private keys
		if info.Size() > 1024*8 {
			return nil
		}
		contents, err := ioutil.ReadFile(path)
		if strings.Contains(string(contents), "PRIVATE KEY") &&
			!strings.Contains(string(contents), "DSA") {
			availableKeys = append(availableKeys, path)
		}
		return nil
	})
	return availableKeys
}

func findSshKeys(root string) []string {

	// Looks in .ssh dir and .vagrant.d dir for ssh keys
	var availableKeys = []string{}
	availableKeys = append(availableKeys, findPrivateKeys(filepath.Join(root, ".ssh"))...)
	availableKeys = append(availableKeys, findPrivateKeys(filepath.Join(root, ".vagrant.d"))...)

	return availableKeys
}

func strip(v string) string {
	return strings.TrimSpace(strings.Trim(v, "\n"))
}

type keychain struct {
	keys []ssh.Signer
}

func (k *keychain) Key(i int) (ssh.PublicKey, error) {
	if i < 0 || i >= len(k.keys) {
		return nil, nil
	}
	return k.keys[i].PublicKey(), nil
}

func (k *keychain) Sign(i int, rand io.Reader, data []byte) (sig []byte, err error) {
	return k.keys[i].Sign(rand, data)
}

func (k *keychain) add(key ssh.Signer) {
	k.keys = append(k.keys, key)
}

func (k *keychain) loadPEM(file string) error {
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	key, err := ssh.ParsePrivateKey(buf)
	if err != nil {
		return err
	}
	k.add(key)
	return nil
}

func filterHost(host string) string {
	var conn string
	token := strings.Split(host, ":")
	if len(token) == 1 {
		conn = host + ":22"
	} else {
		conn = host
	}
	return conn
}

func Sshcmd(host string, command string, background bool, debug bool) {

	keys := new(keychain)
	// Add path to id_rsa file
	err := keys.loadPEM(config.PrivateKey)

	if err != nil {
		panic("Cannot load key: " + err.Error())
	}

	// Assuming the deployed hosts will have a galaxy user created at some
	// point
	username := "galaxy"
	if strings.Contains(config.PrivateKey, "vagrant") {
		username = "vagrant"
	}
	// Switch out username
	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.ClientAuth{
			ssh.ClientAuthKeyring(keys),
		},
	}

	// Workaround for sessoin.Setenv not working
	command = fmt.Sprintf("PATH=$HOME/go/bin:$HOME/go/gopath/bin:/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin:/root/bin %s", command)

	if debug {
		color.Printf("@{b}%s\n", command)
	}

	conn := filterHost(host)

	client, err := ssh.Dial("tcp", conn, config)
	if err != nil {
		color.Printf("@{!r}%s: Failed to connect: %s\n", conn, err.Error())
		return
	}

	session, err := client.NewSession()
	if err != nil {
		color.Printf("@{!r}%s: Failed to create session: %s\n", conn, err.Error())
		return
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		color.Printf("@{!r}%s: Failed to run: %s\n", conn, err.Error())
		color.Printf("@{!r}%s\n", strip(stderr.String()))
		return
	}

	color.Printf("@{!g}%s\n", conn)
	fmt.Print(stdout.String())
}
