package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	sshclient "yaare/internal/adapters/ssh"
)

func main() {
	if len(os.Args) != 3 || !regexp.MustCompile(`^/home/yaaretest/files/yaare-zmodem-[a-f0-9-]+$`).MatchString(os.Args[2]) || (os.Args[1] != "send" && os.Args[1] != "receive") {
		panic("isolated ZMODEM fixture only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	dir, e := os.MkdirTemp("", "yaare-zmodem-pty-")
	if e != nil {
		panic(e)
	}
	defer os.Remove(filepath.Join(dir, "known_hosts"))
	defer os.Remove(dir)
	trust := &sshclient.TrustStore{Path: func() (string, error) { return filepath.Join(dir, "known_hosts"), nil }}
	encoder := json.NewEncoder(os.Stdout)
	var session *sshclient.Session
	ready := make(chan struct{})
	lease := ""
	noSFTP := os.Getenv("YAARE_ZMODEM_NO_SFTP") == "1"
	start := func() {
		if noSFTP {
			check, stop := context.WithTimeout(ctx, 5*time.Second)
			_, closeFiles, e := session.OpenFiles(check)
			stop()
			if e == nil {
				closeFiles()
				fmt.Fprintln(os.Stderr, "unexpected SFTP support")
				cancel()
				return
			}
			fmt.Fprintln(os.Stderr, "SFTP_DENIED_CONFIRMED")
		}
		cmd := "cd -- '" + os.Args[2] + "'; "
		if os.Args[1] == "receive" {
			cmd += "sz -be received.bin"
		} else {
			cmd += "rz -b"
		}
		if os.Getenv("YAARE_ZMODEM_SHELL") == "dash" {
			cmd = "/bin/dash -c '" + strings.ReplaceAll(cmd, "'", "'\\''") + "'"
		}
		if !session.Write([]byte(cmd + "\r")) {
			cancel()
			return
		}
		var e error
		lease, e = session.BeginTerminalTransfer()
		if e != nil {
			cancel()
			return
		}
		close(ready)
	}
	session = sshclient.NewSession(ctx, trust, func(event sshclient.Event) error {
		switch event.Type {
		case "output":
			return encoder.Encode(event)
		case "connected":
			go start()
		case "hostkey":
			if !strings.Contains(","+os.Getenv("YAARE_OPENSSH_FINGERPRINT")+",", ","+event.Challenge.Fingerprint+",") {
				cancel()
				return fmt.Errorf("unrecognized isolated fixture key")
			}
			go session.Resolve(event.Challenge.ID, "once")
		case "directory":
			return encoder.Encode(event)
		}
		return nil
	})
	go func() {
		select {
		case <-ready:
		case <-ctx.Done():
			return
		}
		data := make([]byte, 32768)
		for {
			n, e := os.Stdin.Read(data)
			if n > 0 && !session.WriteTerminalTransfer(lease, data[:n]) {
				cancel()
				return
			}
			if e != nil {
				if e != io.EOF {
					fmt.Fprintln(os.Stderr, "fixture input ended")
				}
				session.Close()
				return
			}
		}
	}()
	cfg := sshclient.Config{Host: "127.0.0.1", Port: 22222, Username: "yaaretest", Authentication: "password", Password: []byte("Yaare-OpenSSH-Test-Only!"), Timeout: 8 * time.Second}
	if noSFTP {
		hop := cfg
		hop.Password = bytes.Clone(cfg.Password)
		cfg.Port = 2223
		cfg.Jumps = []sshclient.Config{hop}
	}
	session.Run(&cfg)
}
