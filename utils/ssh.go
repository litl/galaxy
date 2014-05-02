package utils

import (
	"fmt"
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

	port := "22"
	hostPort := strings.SplitN(host, ":", 2)
	if len(hostPort) > 1 {
		host, port = hostPort[0], hostPort[1]
	}

	cmd := exec.Command("/usr/bin/ssh",
		//"-i", config.PrivateKey,
		"-o", "RequestTTY=yes",
		username+"@"+host,
		"-p", port,
		"-C", "/bin/bash", "-i", "-l", "-c", "'source .bashrc && "+command+"'")

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Connecting to %s...\n", host)
	err = cmd.Wait()
	if err != nil {
		fmt.Printf("Command finished with error: %v\n", err)
	}

}
