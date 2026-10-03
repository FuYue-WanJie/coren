// Command coren is the launcher for the Coren agent framework.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *profileFlag != "" {
		profileName = *profileFlag
	}
	positional := fs.Args()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	prompt := strings.TrimSpace(strings.Join(positional, " "))

	application, err := app.New(context.Background(), cfg, app.Options{
		Profile:         profileName,
		Prompt:          prompt,
		AppVersion:      version,
		WebUpdatePrompt: true,
	})
	if err != nil {
		return err
	}
	defer application.Close()

	// One-shot prompt: run it through the CLI shell without starting the REPL.
	if prompt != "" {
		return runPromptOnce(application, prompt)
	}
	if application.Shell == nil {
		return fmt.Errorf("profile %q has no shell to run", application.Profile.Name)
	}
	// Report a stale extracted WebUI before entering the shell.
	if st, ok := application.WebUIStatus(); ok && st.UpdateAvailable {
		fmt.Fprintf(os.Stderr, "\nWebUI 有新版本可用：你修改时基于版本 %s，当前内置版本 %s。\n打开 WebUI 会提示更新，或在 WebUI 中确认。\n\n",
			displayVersion(st.DiskVersion, st.DiskHash), displayVersion(st.BuiltinVersion, st.BuiltinHash))
	}
	return application.Shell.Run(context.Background())
}

// displayVersion prefers a version string, falling back to a short hash.
func displayVersion(version, hash string) string {
	if version != "" {
		return version
	}
	if len(hash) > 12 {
		return hash[:12]
	}
	if hash == "" {
		return "unknown"
	}
	return hash
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
