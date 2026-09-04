package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// defaultBaseURL is where a Work account lives in production; WORK_URL or
// --base-url overrides it (development, staging).
const defaultBaseURL = "https://work.betterthangood.xyz"

// globalFlags are the flags that precede the command. There is deliberately
// no --token: a token on a command line ends up in shell history and process
// lists, which is not where a live credential belongs — WORK_TOKEN or
// `work auth login` instead, the same reasoning as the API taking Bearer
// only (docs/api.md).
type globalFlags struct {
	baseURL string
	format  string
	help    bool
	version bool
	tty     bool
}

func parseGlobalFlags(args []string) (globalFlags, []string, error) {
	g := globalFlags{}
	for len(args) > 0 {
		arg := args[0]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			break
		}
		args = args[1:]
		switch {
		case arg == "--help" || arg == "-h":
			g.help = true
		case arg == "--version":
			g.version = true
		case strings.HasPrefix(arg, "--base-url="):
			g.baseURL = strings.TrimPrefix(arg, "--base-url=")
		case arg == "--base-url":
			if len(args) == 0 {
				return g, nil, errors.New("--base-url needs a value")
			}
			g.baseURL, args = args[0], args[1:]
		case strings.HasPrefix(arg, "--format="):
			g.format = strings.TrimPrefix(arg, "--format=")
		case arg == "--format":
			if len(args) == 0 {
				return g, nil, errors.New("--format needs a value")
			}
			g.format, args = args[0], args[1:]
		default:
			return g, nil, fmt.Errorf("unknown global flag %q — run `work help`", arg)
		}
	}
	if g.format == "" {
		g.format = "auto"
	}
	if g.format != "auto" && g.format != "json" && g.format != "table" {
		return g, nil, fmt.Errorf("--format must be json or table, got %q", g.format)
	}
	return g, args, nil
}

// effectiveFormat resolves "auto" against the terminal: a person at a
// prompt gets a table, a pipe or an agent gets JSON.
func (g globalFlags) effectiveFormat() string {
	if g.format != "auto" {
		return g.format
	}
	if g.tty {
		return "table"
	}
	return "json"
}

// client builds an authenticated client from, in order of precedence:
// --base-url / WORK_URL / the config file / the default host, and
// WORK_TOKEN / the config file.
func (g globalFlags) client() (*client, error) {
	token := os.Getenv("WORK_TOKEN")
	if token == "" {
		token = loadConfig().Token
	}
	return newClient(g.resolvedBaseURL(), token)
}

func (g globalFlags) resolvedBaseURL() string {
	if g.baseURL != "" {
		return g.baseURL
	}
	if env := os.Getenv("WORK_URL"); env != "" {
		return env
	}
	if saved := loadConfig().BaseURL; saved != "" {
		return saved
	}
	return defaultBaseURL
}

// fileConfig is ~/.config/work/config.json — what `work auth login` saved.
type fileConfig struct {
	Token   string `json:"token"`
	BaseURL string `json:"base_url"`
}

// configPath honours WORK_CONFIG first — one agent container can hold
// several accounts' tokens side by side — then the platform's config dir.
func configPath() string {
	if override := os.Getenv("WORK_CONFIG"); override != "" {
		return override
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "work", "config.json")
}

// loadConfig answers an empty config when none exists — a first run before
// `work auth login` is a normal state, not an error.
func loadConfig() fileConfig {
	payload, err := os.ReadFile(configPath())
	if err != nil {
		return fileConfig{}
	}
	var config fileConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return fileConfig{}
	}
	return config
}

func runAuth(g globalFlags, args []string, u ui) int {
	if len(args) == 0 {
		return u.usage("auth needs a subcommand — login or logout")
	}
	switch args[0] {
	case "login":
		return authLogin(g, args[1:], u)
	case "logout":
		return authLogout(u)
	default:
		return u.usage("auth has no %q — login or logout", args[0])
	}
}

// authLogin takes a token from the prompt or piped stdin — never a flag,
// because a token in argv lands in shell history and in every process list
// on the machine — verifies it against the API before saving, so a typo'd
// token fails here rather than mid-task, and stores it mode 0600.
func authLogin(g globalFlags, args []string, u ui) int {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	baseURLFlag := fs.String("base-url", "", "save this host too (self-hosting or staging)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(u.stdout, "Usage: work auth login [--base-url URL]")
			fmt.Fprintln(u.stdout, "  Reads the token from the prompt, or from stdin when piped.")
			return 0
		}
		fmt.Fprintf(u.stderr, "work: %v\n", err)
		return 2
	}

	token, err := readToken(u)
	if err != nil {
		fmt.Fprintf(u.stderr, "work: %v\n", err)
		return 2
	}

	baseURL := g.resolvedBaseURL()
	if *baseURLFlag != "" {
		baseURL = *baseURLFlag
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return u.usage("%q is not a URL", baseURL)
	}

	probe, err := newClient(baseURL, token)
	if err != nil {
		fmt.Fprintf(u.stderr, "work: %v\n", err)
		return 1
	}
	if _, err := probe.do("GET", "/contacts", url.Values{"per_page": {"1"}}, nil); err != nil {
		u.printError(err)
		return 1
	}

	// The saved host carries forward: a login that only rotates a token must
	// not quietly move the CLI back to the default host, which would point
	// the new credential at a server it was never issued for.
	config := fileConfig{Token: token, BaseURL: loadConfig().BaseURL}
	if *baseURLFlag != "" {
		config.BaseURL = baseURL
	}
	if err := saveConfig(config); err != nil {
		fmt.Fprintf(u.stderr, "work: could not save the token: %v\n", err)
		return 1
	}
	fmt.Fprintf(u.stdout, "Authenticated against %s; token saved to %s.\n", baseURL, configPath())
	return 0
}

func authLogout(u ui) int {
	err := os.Remove(configPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(u.stderr, "work: could not remove %s: %v\n", configPath(), err)
		return 1
	}
	fmt.Fprintln(u.stdout, "Logged out — the saved token is gone. Revoke it in Settings → API tokens too if it may have leaked.")
	return 0
}

// readToken reads the token from the prompt when interactive, or one line
// of stdin when piped — `pass work/token | work auth login` and
// `work auth login` at a prompt both work.
func readToken(u ui) (string, error) {
	if u.tty {
		fmt.Fprint(u.stderr, "Personal access token (Settings → API tokens): ")
	}
	line, err := bufio.NewReader(u.stdin).ReadString('\n')
	token := strings.TrimSpace(line)
	if err != nil && token == "" {
		return "", errors.New("no token given — pipe it on stdin, or run at a terminal")
	}
	if token == "" {
		return "", errors.New("no token given")
	}
	return token, nil
}

func saveConfig(config fileConfig) error {
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath()), 0o700); err != nil {
		return err
	}
	// 0600: the file holds a live credential that reads the account's whole
	// book of business; no other user on the machine gets a look.
	return os.WriteFile(configPath(), payload, 0o600)
}
