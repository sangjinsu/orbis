package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// errUsage marks a command-line usage error. The subcommand prints its own
// usage line to stderr; main maps this sentinel to exit code 2.
var errUsage = errors.New("usage")

const defaultAddr = ":8080"

// usagef writes a usage line to stderr and returns errUsage.
func usagef(format string, args ...any) error {
	fmt.Fprintf(os.Stderr, "usage: "+format+"\n", args...)
	return errUsage
}

// commonFlags are shared by every HTTP subcommand. Resolution order:
// flag > environment (ORBIS_ADDR / ORBIS_TOKEN) > default. config.Load is
// deliberately not used here: it validates server-side LLM credentials that a
// client shell does not need, and its .env-over-env merge would defeat shell
// overrides.
type commonFlags struct {
	addr    string
	token   string
	timeout time.Duration
	asJSON  bool
}

func registerCommon(fs *flag.FlagSet) *commonFlags {
	f := &commonFlags{}
	fs.StringVar(&f.addr, "addr", "", "server address (default $ORBIS_ADDR or :8080)")
	fs.StringVar(&f.token, "token", "", "bearer token (default $ORBIS_TOKEN)")
	fs.DurationVar(&f.timeout, "timeout", 10*time.Second, "request timeout")
	fs.BoolVar(&f.asJSON, "json", false, "print the raw JSON response")
	return f
}

func resolveAddr(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("ORBIS_ADDR"); env != "" {
		return env
	}
	return defaultAddr
}

func (f *commonFlags) client() *apiClient {
	token := f.token
	if token == "" {
		token = os.Getenv("ORBIS_TOKEN")
	}
	return &apiClient{
		BaseURL: httpBaseURLFromAddr(resolveAddr(f.addr)),
		Token:   token,
		HTTP:    &http.Client{Timeout: f.timeout},
	}
}

// stringList is a repeatable flag value for the proposal list fields. Not
// passing the flag leaves the field unchanged (nil pointer); passing it once
// with an empty string clears the list to []; empty values among non-empty
// ones are ignored.
type stringList struct {
	values []string
	set    bool
}

func (s *stringList) String() string { return strings.Join(s.values, ", ") }

func (s *stringList) Set(value string) error {
	s.set = true
	if value != "" {
		s.values = append(s.values, value)
	}
	return nil
}

func (s *stringList) toPtr() *[]string {
	if !s.set {
		return nil
	}
	if len(s.values) == 0 {
		empty := []string{}
		return &empty
	}
	return &s.values
}

// printRawJSON emits the server response body unchanged (single trailing
// newline), so scripted callers see the wire encoding without re-marshaling.
func printRawJSON(out io.Writer, body []byte) {
	fmt.Fprintln(out, strings.TrimSpace(string(body)))
}
