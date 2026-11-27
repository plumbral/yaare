package main

import (
	"fmt"
	"os"
	"os/signal"
	"yaare/internal/testutil/sshfixture"
)

func main() {
	server, err := sshfixture.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Unable to start loopback SSH fixture")
		os.Exit(1)
	}
	defer server.Close()
	fmt.Println("SSH fixture:", server.Address())
	fmt.Println("User:", sshfixture.Username)
	fmt.Println("Host fingerprint:", server.Fingerprint())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	defer signal.Stop(stop)
	<-stop
}
