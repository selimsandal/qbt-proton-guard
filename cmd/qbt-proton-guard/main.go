package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/selimsandal/qbt-proton-guard/internal/buildinfo"
	"github.com/selimsandal/qbt-proton-guard/internal/guard"
	"github.com/selimsandal/qbt-proton-guard/internal/proton"
	"github.com/selimsandal/qbt-proton-guard/internal/qbittorrent"
	"github.com/selimsandal/qbt-proton-guard/internal/selfupdate"
	"github.com/selimsandal/qbt-proton-guard/internal/statusapp"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "once":
		once()
	case "status":
		status()
	case "details":
		state, err := guard.ReadRuntimeState()
		fmt.Println(statusapp.FullStatus(state, err, time.Now()))
	case "ui":
		runUI()
	case "icon-login":
		if len(os.Args) != 3 {
			log.Fatal("usage: qbt-proton-guard icon-login status|on|off")
		}
		switch os.Args[2] {
		case "status":
			enabled, err := guard.StatusAtLogin()
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println(enabled)
		case "on", "off":
			if err := guard.SetStatusAtLogin(os.Args[2] == "on"); err != nil {
				log.Fatal(err)
			}
		default:
			log.Fatal("usage: qbt-proton-guard icon-login status|on|off")
		}
	case "install":
		if err := guard.Install(); err != nil {
			log.Fatal(err)
		}
	case "install-update":
		err := guard.Install()
		selfupdate.Finish(err)
		if err != nil {
			log.Fatal(err)
		}
	case "update-background":
		if err := selfupdate.StartBackground(); err != nil {
			log.Fatal(err)
		}
	case "update-result":
		if result, ok := selfupdate.TakeResult(); ok {
			if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
				log.Fatal(err)
			}
		}
	case "uninstall":
		if err := guard.Uninstall(); err != nil {
			log.Fatal(err)
		}
	case "update":
		flags := flag.NewFlagSet("update", flag.ExitOnError)
		check := flags.Bool("check", false, "check for updates without installing")
		_ = flags.Parse(os.Args[2:])
		if flags.NArg() != 0 {
			log.Fatal("usage: qbt-proton-guard update [--check]")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := selfupdate.Run(ctx, buildinfo.Version, *check); err != nil {
			log.Fatal(err)
		}
	case "version", "--version", "-v":
		fmt.Println(buildinfo.Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runUI() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := statusapp.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) {
	flags := flag.NewFlagSet("run", flag.ExitOnError)
	interval := flags.Duration("interval", 500*time.Millisecond, "guard check interval")
	_ = flags.Parse(args)
	if *interval < 100*time.Millisecond {
		log.Fatal("interval must be at least 100ms")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := guard.Run(ctx, *interval); err != nil {
		log.Fatal(err)
	}
}

func once() {
	result, err := guard.Enforce(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result)
}

func status() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	service, serviceErr := guard.ServiceStatus()
	runtimeState, runtimeErr := guard.ReadRuntimeState()
	tunnel, tunnelErr := proton.Detect(ctx)
	config, configErr := qbittorrent.FindConfig()
	running, processErr := qbittorrent.Running(ctx)

	if serviceErr != nil {
		fmt.Printf("Guard service: unknown (%v)\n", serviceErr)
	} else {
		fmt.Printf("Guard service: %s\n", service)
	}
	if runtimeErr != nil {
		fmt.Printf("Guard heartbeat: unavailable (%v)\n", runtimeErr)
	} else {
		age := time.Since(runtimeState.UpdatedAt).Round(time.Second)
		if age < 0 {
			age = 0
		}
		health := "healthy"
		if !runtimeState.Healthy {
			health = "degraded"
		}
		fmt.Printf("Guard heartbeat: %s, %s ago (PID %d)\n", health, age, runtimeState.PID)
		fmt.Printf("Guard state: %s\n", runtimeState.Message)
		if runtimeState.ForwardedPort != 0 && runtimeState.PortForwardingError == "" {
			fmt.Printf("Proton forwarded port: %d\n", runtimeState.ForwardedPort)
		} else if runtimeState.PortForwardingError != "" {
			fmt.Printf("Proton port forwarding: unavailable (%s)\n", runtimeState.PortForwardingError)
		} else {
			fmt.Println("Proton port forwarding: inactive")
		}
	}
	if errors.Is(tunnelErr, proton.ErrDisconnected) {
		fmt.Println("Proton VPN: disconnected")
	} else if tunnelErr != nil {
		fmt.Printf("Proton VPN: unknown (%v)\n", tunnelErr)
	} else {
		fmt.Printf("Proton VPN: connected on %s (%s)\n", tunnel.Interface, tunnel.Address)
	}
	if configErr != nil {
		fmt.Printf("qBittorrent config: unavailable (%v)\n", configErr)
	} else {
		safety, err := qbittorrent.ReadSafety(config)
		if err != nil {
			fmt.Printf("qBittorrent config: %s (unreadable: %v)\n", config, err)
		} else {
			fmt.Printf("qBittorrent binding: %q (%q)\n", safety.Interface, safety.Address)
			fmt.Printf("qBittorrent port: %d\n", safety.Port)
			fmt.Printf("Local peer discovery disabled: %t\n", safety.LocalPeerDiscoveryDisabled)
			fmt.Printf("Router UPnP/NAT-PMP disabled: %t\n", safety.RouterPortForwardingDisabled)
			fmt.Printf("qBittorrent config: %s\n", config)
		}
	}
	if processErr != nil {
		fmt.Printf("qBittorrent process: unknown (%v)\n", processErr)
	} else if running {
		fmt.Println("qBittorrent process: running")
	} else {
		fmt.Println("qBittorrent process: stopped")
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `qbt-proton-guard keeps qBittorrent fail-closed to Proton VPN.

Usage:
  qbt-proton-guard run [--interval 500ms]  Run the foreground guard
  qbt-proton-guard once                    Enforce the safe binding once
  qbt-proton-guard status                  Show Proton and qBittorrent state
  qbt-proton-guard details                 Show the last diagnostic snapshot
  qbt-proton-guard ui                      Open the status icon (Linux/Windows)
  qbt-proton-guard icon-login status|on|off Show or change icon login behavior
  qbt-proton-guard install                 Install and start the user service
  qbt-proton-guard uninstall               Remove the user service
  qbt-proton-guard update [--check]         Install or check the latest release
  qbt-proton-guard version                 Print the version`)
}
