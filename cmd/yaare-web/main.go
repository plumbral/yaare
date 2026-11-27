package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/term"
	"yaare/internal/webserver"
)

func main() {
	args := os.Args[1:]
	initialize := len(args) > 0 && args[0] == "init"
	flags := flag.CommandLine
	if initialize {
		flags = flag.NewFlagSet("yaare-web init", flag.ExitOnError)
		args = args[1:]
	}
	var c webserver.Config
	flags.StringVar(&c.Root, "data", envOr("YAARE_DATA_DIR", "./server-data"), "server data directory")
	flags.StringVar(&c.KeyFile, "key-file", envOr("YAARE_KEY_FILE", "/run/secrets/yaare_key"), "32-byte instance key file")
	flags.StringVar(&c.User, "user", "admin", "initial username")
	flags.StringVar(&c.PasswordFile, "password-file", "", "read the initial password from a file, or - for stdin")
	var address string
	var health bool
	if !initialize {
		flags.StringVar(&address, "listen", "127.0.0.1:4180", "HTTP listen address")
		flags.StringVar(&c.Assets, "assets", "./build/browser", "built browser assets")
		flags.StringVar(&c.Origin, "origin", envOr("YAARE_ORIGIN", "http://127.0.0.1:4180"), "public origin")
		flags.StringVar(&c.ResetPasswordFile, "reset-owner-password-file", "", "replace the password on this startup")
		flags.BoolVar(&health, "healthcheck", false, "check local readiness and exit")
	}
	flags.Parse(args)
	if flags.NArg() != 0 {
		log.Fatal("Usage: yaare-web [init] [flags]")
	}
	if initialize {
		if err := webserver.Initialize(c, promptPassword); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Instance ready. Start Yaare with docker compose up -d.")
		return
	}
	if health {
		public, e := url.Parse(c.Origin)
		if e != nil {
			os.Exit(1)
		}
		req, e := http.NewRequest("GET", "http://"+address+"/readyz", nil)
		if e != nil {
			os.Exit(1)
		}
		req.Host = public.Host
		client := http.Client{Timeout: 3 * time.Second}
		res, e := client.Do(req)
		if e != nil {
			os.Exit(1)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	var e error
	c.Root, e = filepath.Abs(c.Root)
	if e != nil {
		log.Fatal("数据目录无效。")
	}
	c.Assets, e = filepath.Abs(c.Assets)
	if e != nil {
		log.Fatal("资源目录无效。")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	handler, e := webserver.Open(ctx, c)
	if e != nil {
		log.Fatal(e)
	}
	defer handler.Close()
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		bounded, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(bounded)
	}()
	log.Printf("Yaare browser server: %s", c.Origin)
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal("浏览器服务未能启动，请检查监听地址。")
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func promptPassword() ([]byte, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("use an interactive terminal or pass --password-file to init")
	}
	fmt.Fprint(os.Stderr, "Administrator password (12–72 bytes): ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, err
	}
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	defer clear(second)
	if err != nil || !bytes.Equal(first, second) {
		clear(first)
		return nil, errors.New("password confirmation failed; run init again")
	}
	return first, nil
}
