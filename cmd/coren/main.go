// Command coren is the launcher for the Coren agent framework.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"coren/internal/app"
	"coren/internal/config"
	"coren/internal/profile"
	"coren/pkg/shell"
)

var usage = `Coren - a small self-hosted agent framework

Usage:
  coren [--profile NAME] [prompt]   Run the profile; with a prompt, run it once.
  coren serve                       Run the web profile (HTTP API + WebUI).
  coren run [prompt]                Run the cli profile (interactive or one-shot).
  coren profile                     List available profiles.
  coren config <action>             Manage configuration (init | show | path).
  coren mcp <action>                Manage remote MCP servers (list | add | remove | test).
  coren models [--filter SUBSTR]    List models available from the provider.
  coren version                     Print the version.
  coren help                        Show this help.

Profiles: ` + profileNames() + `

Config:
  Settings load from (low to high precedence):
    defaults  <  ./coren.json  <  user config  <  environment variables
  Run "coren config init" to create ./coren.json, then edit it.

Environment:
  COREN_API          chat | responses      (default chat)
  COREN_BASE_URL     OpenAI-compatible root
  COREN_API_KEY      bearer token
  COREN_MODEL        model name            (default gpt-4o-mini)
  COREN_SYSTEM       system prompt
  COREN_WORKDIR      tool working directory
  COREN_ADDR         server address        (default 127.0.0.1:8787)
  COREN_SESSION_DIR  session log directory
  COREN_NO_PERSIST   set to disable session persistence
`

const version = "0.1.0"

func profileNames() string {
	return strings.Join(profile.Names(), ", ")
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "config":
		exitOn(runConfig(os.Args[2:]))
	case "mcp":
		exitOn(runMCP(os.Args[2:]))
	case "models":
		exitOn(runModels(os.Args[2:]))
	case "profile", "profiles":
		listProfiles()
	case "version", "--version", "-v":
		fmt.Println("Coren", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		exitOn(run(os.Args[1:]))
	}
}

func exitOn(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func listProfiles() {
	for _, p := range profile.All() {
		fmt.Printf("%-10s %s\n", p.Name, p.Description)
	}
}

// run resolves the profile and shell from args, boots the app, and runs the shell.
func run(args []string) error {
	// Strip a leading subcommand so flags may follow it (e.g. "serve --addr ...").
	profileName := ""
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			profileName = "web"
			args = args[1:]
		case "run":
			profileName = "cli"
			args = args[1:]
		}
	}

	fs := flag.NewFlagSet("coren", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", "profile to run")
	addr := fs.String("addr", "", "listen address, e.g. 0.0.0.0:8787")
	password := fs.String("password", "", "web login password (required when listening off loopback)")
	noUI := fs.Bool("no-ui", false, "serve the HTTP API only, without the browser UI")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profileFlag != "" {
		profileName = *profileFlag
	}
	// --no-ui without an explicit profile selects the API-only composition.
	if *noUI && *profileFlag == "" {
		profileName = "api"
	}
	positional := fs.Args()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if *password != "" {
		cfg.Password = *password
	}
	if err := checkExposure(cfg); err != nil {
		return err
	}
	prompt := strings.TrimSpace(strings.Join(positional, " "))

	application, err := app.New(context.Background(), cfg, app.Options{
		Profile: profileName,
		Prompt:  prompt,
	})
	if err != nil {
		return err
	}
	defer application.Close()

	// One-shot prompt: run it through the CLI shell without starting the REPL.
	if prompt != "" {
		return runPromptOnce(application, prompt)
	}

	// A shell owns the process lifetime when present (CLI REPL, WebUI).
	if application.Shell != nil {
		return application.Shell.Run(context.Background())
	}
	// No shell: the HTTP server or another service runs in the background, so
	// block until interrupted.
	return waitForSignal()
}

// waitForSignal blocks until the process receives an interrupt or SIGTERM.
func waitForSignal() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	return nil
}

// checkExposure refuses to start when a non-loopback address is used without a
// password, since that would expose the agent to anyone who can reach it.
func checkExposure(cfg config.Config) error {
	if isLoopbackAddr(cfg.Addr) || cfg.Password != "" {
		return nil
	}
	return fmt.Errorf("refusing to listen on %q without a password; set --password, COREN_PASSWORD, or a password in %s", cfg.Addr, config.FileName)
}

// isLoopbackAddr reports whether an address only accepts local connections.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port or malformed: treat as local only if it looks loopback.
		host = addr
	}
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// runPromptOnce drives a single turn through whichever shell supports it.
func runPromptOnce(application *app.App, prompt string) error {
	if application.Shell == nil {
		return fmt.Errorf("profile %q has no shell; use a shell profile for prompts", application.Profile.Name)
	}
	prompter, ok := application.Shell.(shell.Prompter)
	if !ok {
		return fmt.Errorf("shell %q does not support one-shot prompts", application.Shell.Name())
	}
	return prompter.Prompt(context.Background(), prompt)
}
