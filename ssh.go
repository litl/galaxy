package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
)

func SSHCmd(host string, command string, background bool, debug bool) {

	// Assuming the deployed hosts will have a galaxy user created at some
	// point
	username := "galaxy"
	if strings.Contains(host, "127.0.0.1:2222") {
		username = "vagrant"
	}

	hostPort := strings.SplitN(host, ":", 2)
	host, port := hostPort[0], hostPort[1]
	cmd := exec.Command("/usr/bin/ssh",
		//"-i", config.PrivateKey,
		"-o", "RequestTTY=yes",
		username+"@"+host,
		"-p", port,
		"-C", "/bin/bash", "-i", "-l", "-c", "'"+command+"'")

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Connecting to %s...", host)
	err = cmd.Wait()
	if err != nil {
		log.Printf("Command finished with error: %v", err)
	}

}
