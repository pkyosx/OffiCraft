package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// The secret routes are off the MCP surface on purpose: a value revealed
// through a tool result would land in the model's context window. This CLI is
// the only reader, and every path below hands a value to a child process, a
// file, or a masking writer — never to out/errOut.

const (
	selfSecretsPath      = "/api/self/secrets"
	selfSecretRevealPath = "/api/self/secrets/reveal"
	secretRevealMaxIDs   = 16
	secretMask           = "***"
)

func secretUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent secret <list | show | run | write> [flags]

Uses the secrets the owner stored on the station (the owner manages them in the
Secrets tab). A secret is addressed by its id (sec-…); the name is only a label
and need not be unique. Read the note: it is the owner's instruction for how
that secret may be used.

A value the owner gave on a secret reply card you opened is addressed by the
card's id (rc-…), on run and write only, and only you, its author, can retrieve
it. It is destroyed after a retention period that starts at your first
retrieval (or, if you never retrieve it, after a longer deadline); the first
retrieval does not end it, and later ones do not extend it. If a step can fail,
write the value to a file first. When you are done, delete any file you wrote
and end the retention early with expire_reply_card. If the owner replies with a
message instead of a value, or the value expired, open a new card.

NO COMMAND EVER PRINTS A VALUE. A value only ever reaches a child process's
environment (run) or a 0600 file (write). Never put one on a command line (argv
is visible to every process on the machine). Do not echo it, paste it into chat,
or write it anywhere else. The owner sees when each secret was last retrieved.

subcommands:
  list                              every secret: id, name, note
  show <ID>                         one secret: id, name, note
  run --env VAR=<ID> [...] -- cmd   run cmd with the values in its environment;
                                    every value in its output is shown as ***;
                                    <ID> is sec-… or a secret card's rc-…
  run --stdin-json <ID> -- cmd      run cmd with the value on stdin as a JSON string
  write <ID> --file <path>          write the value to a new 0600 file; <ID> is
                                    sec-… or a secret card's rc-…

examples:
  ocagent secret list
  ocagent secret run --env GITHUB_TOKEN=sec-00000000000b -- gh api user
  ocagent secret write sec-00000000000b --file ./deploy.key
  ocagent secret write rc-00000000000b --file ./deploy.key

Each subcommand takes --help for its flags and exit codes.
`)
}

func secretListUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent secret list

Lists every secret on the station: id, name and the owner's note. Never a value.

stdout: one block per secret, oldest first —
  <id>  <name>
      <note, indented; omitted when empty>
separated by a blank line. Nothing at all when the station holds no secret.

Exit codes:
  0  listed
  1  the request never got an answer (network)
  2  usage: --help itself, or any flag or argument
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403 (only agents and admin agents may use secrets)
  5  any other HTTP status, or a body that is not a secret list
`)
}

func secretShowUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent secret show <ID>

Shows one secret's id, name and note. Never the value.

Exit codes:
  0  shown
  1  the request never got an answer (network)
  2  usage: --help itself, any flag, missing <ID>, or more than one argument
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403
  4  no secret with that id on the station
  5  any other HTTP status, or a body that is not a secret list
`)
}

func secretRunUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent secret run [--env VAR=<ID> ...] [--stdin-json <ID>] -- <cmd> [args...]

Runs <cmd> with each VAR set to the value of <ID>, on top of this process's own
environment: a secret (sec-…) or a secret reply card you opened (rc-…). <cmd> is
resolved first, so a command that cannot be found does not use up a card's
value. All the values are fetched in ONE request before the command starts (at
most 16 distinct ids); if any id is refused nothing runs.

The first retrieval of a card's value starts its retention: the value is
destroyed when that ends, and until then you may retrieve it again.

The command's stdout and stderr are passed through with every occurrence of
every value replaced by ***. A value split across two writes is still caught,
which means output can lag by up to one value's length until more output or
the end of the stream arrives. stdin is passed through untouched unless --stdin-json is given.

Never put a value on the command line yourself (argv is visible to every
process on the machine): let the command read it from VAR.

--env VAR=<ID>      repeatable. VAR must look like a shell variable name
                    ([A-Za-z_][A-Za-z0-9_]*) and may appear once; one <ID> may
                    feed several VARs.
--stdin-json <ID>   give <cmd> the value on stdin as a JSON string, for commands
                    that read a JSON value there. At least one of --env and
                    --stdin-json is required.

examples:
  ocagent secret run --env NPM_TOKEN=sec-00000000000b -- npm publish
  ocagent secret run --stdin-json rc-00000000000b -- sops set --value-stdin prod.enc.yaml '["db"]["password"]'

Exit codes: the command's own exit code once it has started (128+N when it
was killed by signal N). Before it starts:
  1  the request never got an answer (network), or <cmd> could not be found or
     started (nothing is fetched when it cannot be found)
  2  usage: --help itself, neither --env nor --stdin-json, a malformed --env or
     --stdin-json, more than 16 distinct ids, or no <cmd>
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403
  4  at least one id is unknown on the station (nothing was revealed)
  5  any other HTTP status, or a body that is not the requested secrets
  7  a card holds no retrievable value: not provided, already destroyed, or its
     task is closed; the station's message says which card (nothing was revealed)
A diagnostic on stderr, prefixed [ocagent] secret run:, tells these apart from
the command's own codes.
`)
}

func secretWriteUsage(w io.Writer) {
	fmt.Fprint(w, `usage: ocagent secret write <ID> --file <path> [--force]

Writes the value of <ID> to <path> with permissions 0600, for tools that only
read a credential from a file: a secret (sec-…) or a secret reply card you
opened (rc-…). The first retrieval of a card's value starts its retention. The
file is written to a temporary name in
the same directory and moved into place, so <path> never holds a partial value.

--file <path>  destination. Its directory must already exist (it is not
               created).
--force        replace <path> if it already exists. Without it an existing
               path is refused before anything is fetched.

stdout on success: one line, 已寫入 <path>. The value is never printed.
Delete the file when you are done with it.

Exit codes:
  0  written
  1  the request never got an answer (network), the directory is missing, or
     the file could not be written (the temporary file is removed)
  2  usage: --help itself, missing <ID> or --file, or extra arguments
  3  no OC_TOKEN or no OC_BASE in the environment, or the server answered
     401/403
  4  no secret with that id on the station
  5  any other HTTP status, or a body that is not the requested secret
  6  <path> already exists and --force was not given
  7  a card holds no retrievable value: not provided, already destroyed, or its
     task is closed; the station's message says which card (nothing was revealed)
`)
}

type selfSecret struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note"`
}

type revealedSecret struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type secretRequestError struct {
	code int
	msg  string
}

func cmdSecret(argv []string, cfg Config, in io.Reader, out, errOut io.Writer) int {
	if len(argv) == 0 {
		secretUsage(out)
		return 2
	}
	sub, rest := argv[0], argv[1:]
	switch sub {
	case "-h", "--help", "help":
		secretUsage(out)
		return 0
	case "list":
		fs := flag.NewFlagSet("ocagent secret list", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { secretListUsage(out) }
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() != 0 {
			fmt.Fprintln(errOut, "[ocagent] secret list: takes no arguments")
			return 2
		}
		return cmdSecretList(defaultHTTPClient(), cfg, out, errOut)
	case "show":
		fs := flag.NewFlagSet("ocagent secret show", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { secretShowUsage(out) }
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if fs.NArg() != 1 {
			fmt.Fprintln(errOut, "[ocagent] secret show: exactly one <ID> argument is required")
			return 2
		}
		if secretCardIDShape.MatchString(fs.Arg(0)) {
			fmt.Fprintf(errOut, "[ocagent] secret show: %q is a reply card, and show only reads stored secrets; "+
				"read the card with get_reply_card and retrieve its value with `ocagent secret run` or `write`\n", fs.Arg(0))
			return 2
		}
		if !secretIDShape.MatchString(fs.Arg(0)) {
			fmt.Fprintf(errOut, "[ocagent] secret show: %q is not a secret id (sec- followed by 12 lowercase hex digits)\n", fs.Arg(0))
			return 2
		}
		return cmdSecretShow(defaultHTTPClient(), cfg, fs.Arg(0), out, errOut)
	case "run":
		fs := flag.NewFlagSet("ocagent secret run", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { secretRunUsage(out) }
		var envs envFlags
		fs.Var(&envs, "env", "VAR=<ID>, repeatable")
		stdinJSON := fs.String("stdin-json", "", "give the command this <ID>'s value on stdin as a JSON string")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		return runSecretCommand(defaultHTTPClient(), cfg, envs, *stdinJSON, fs.Args(), os.Environ(), in, out, errOut)
	case "write":
		fs := flag.NewFlagSet("ocagent secret write", flag.ContinueOnError)
		fs.SetOutput(out)
		fs.Usage = func() { secretWriteUsage(out) }
		file := fs.String("file", "", "destination path (its directory must exist)")
		force := fs.Bool("force", false, "replace an existing file")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		args := fs.Args()
		if len(args) >= 1 {
			if err := fs.Parse(args[1:]); err != nil {
				return 2
			}
		}
		if len(args) < 1 || fs.NArg() != 0 || *file == "" {
			fmt.Fprintln(errOut, "[ocagent] secret write: exactly one <ID> and --file <path> are required")
			fmt.Fprintln(errOut, "usage: ocagent secret write <ID> --file <path> [--force]")
			return 2
		}
		if !revealableIDShape(args[0]) {
			fmt.Fprintf(errOut, "[ocagent] secret write: %q is not a secret id (sec- or rc- followed by 12 lowercase hex digits)\n", args[0])
			return 2
		}
		return cmdSecretWrite(defaultHTTPClient(), cfg, args[0], *file, *force, out, errOut)
	default:
		fmt.Fprintf(errOut, "[ocagent] secret: unknown subcommand %q\n\n", sub)
		secretUsage(errOut)
		return 2
	}
}

// OC_BASE CLASSIFICATION: GUARDED — refuse, exit 3 (every secret subcommand).
func secretPreflight(cfg Config, label string, errOut io.Writer) bool {
	if cfg.Token == "" {
		fmt.Fprintf(errOut, "[ocagent] %s: no OC_TOKEN configured — cannot make an authed request.\n", label)
		return false
	}
	return !warnMissingBase(cfg, label, errOut)
}

// statusError maps a non-200 answer to an exit code. Only the error envelope's
// message is quoted: a non-200 never carries a value, but nothing else of the
// body is trusted to stay that way.
func statusError(status int, text string) *secretRequestError {
	var env struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(text), &env)
	msg := env.Error.Message
	if msg == "" {
		msg = "no error message"
	}
	switch {
	case status == 0:
		return &secretRequestError{1, "request failed (network)"}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return &secretRequestError{3, fmt.Sprintf("auth rejected (HTTP %d): %s", status, msg)}
	case status == http.StatusNotFound:
		return &secretRequestError{4, fmt.Sprintf("not found (HTTP 404): %s", msg)}
	case status == http.StatusConflict:
		return &secretRequestError{7, fmt.Sprintf("refused (HTTP 409): %s", msg)}
	default:
		return &secretRequestError{5, fmt.Sprintf("unexpected HTTP %d: %s", status, msg)}
	}
}

func fetchSelfSecrets(client httpClient, cfg Config) ([]selfSecret, *secretRequestError) {
	status, text := httpRequest(client, http.MethodGet, cfg.Base+selfSecretsPath, cfg.Token, nil)
	if status != http.StatusOK {
		if status == 0 {
			return nil, &secretRequestError{1, "request failed (network): " + text}
		}
		return nil, statusError(status, text)
	}
	var body struct {
		Secrets *[]selfSecret `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil || body.Secrets == nil {
		return nil, &secretRequestError{5, "200 but the body is not a secret list"}
	}
	return *body.Secrets, nil
}

// revealSecrets is the ONE request that answers with values. Its error strings
// never include the response body.
func revealSecrets(client httpClient, cfg Config, ids []string) (map[string]string, *secretRequestError) {
	status, text := httpRequest(client, http.MethodPost, cfg.Base+selfSecretRevealPath, cfg.Token, map[string]any{"ids": ids})
	if status != http.StatusOK {
		if status == 0 {
			return nil, &secretRequestError{1, "request failed (network): " + text}
		}
		return nil, statusError(status, text)
	}
	var body struct {
		Secrets []revealedSecret `json:"secrets"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return nil, &secretRequestError{5, "200 but the body is not the requested secrets"}
	}
	values := make(map[string]string, len(body.Secrets))
	for _, s := range body.Secrets {
		values[s.ID] = s.Value
	}
	for _, id := range ids {
		if _, ok := values[id]; !ok {
			return nil, &secretRequestError{5, "200 but the answer is missing " + id}
		}
	}
	return values, nil
}

func printSecretBlock(out io.Writer, s selfSecret) {
	fmt.Fprintf(out, "%s  %s\n", s.ID, s.Name)
	if s.Note == "" {
		return
	}
	for _, line := range strings.Split(s.Note, "\n") {
		fmt.Fprintf(out, "    %s\n", line)
	}
}

func cmdSecretList(client httpClient, cfg Config, out, errOut io.Writer) int {
	if !secretPreflight(cfg, "secret list", errOut) {
		return 3
	}
	secrets, rerr := fetchSelfSecrets(client, cfg)
	if rerr != nil {
		fmt.Fprintf(errOut, "[ocagent] secret list: %s\n", rerr.msg)
		return rerr.code
	}
	for i, s := range secrets {
		if i > 0 {
			fmt.Fprintln(out)
		}
		printSecretBlock(out, s)
	}
	return 0
}

func cmdSecretShow(client httpClient, cfg Config, id string, out, errOut io.Writer) int {
	if !secretPreflight(cfg, "secret show", errOut) {
		return 3
	}
	secrets, rerr := fetchSelfSecrets(client, cfg)
	if rerr != nil {
		fmt.Fprintf(errOut, "[ocagent] secret show: %s\n", rerr.msg)
		return rerr.code
	}
	for _, s := range secrets {
		if s.ID == id {
			fmt.Fprintf(out, "id:    %s\nname:  %s\n", s.ID, s.Name)
			if s.Note == "" {
				fmt.Fprintln(out, "note:  (none)")
				return 0
			}
			fmt.Fprintln(out, "note:")
			for _, line := range strings.Split(s.Note, "\n") {
				fmt.Fprintf(out, "    %s\n", line)
			}
			return 0
		}
	}
	fmt.Fprintf(errOut, "[ocagent] secret show: no secret %q on the station (see: ocagent secret list)\n", id)
	return 4
}

type envFlags []string

func (e *envFlags) String() string     { return strings.Join(*e, ",") }
func (e *envFlags) Set(v string) error { *e = append(*e, v); return nil }

var envVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var (
	secretIDShape     = regexp.MustCompile(`^sec-[0-9a-f]{12}$`)
	secretCardIDShape = regexp.MustCompile(`^rc-[0-9a-f]{12}$`)
)

func revealableIDShape(id string) bool {
	return secretIDShape.MatchString(id) || secretCardIDShape.MatchString(id)
}

type envBinding struct{ name, id string }

func parseEnvBindings(envs []string) ([]envBinding, []string, error) {
	if len(envs) == 0 {
		return nil, nil, errors.New("at least one --env VAR=<ID> or --stdin-json <ID> is required")
	}
	seenVar := map[string]bool{}
	seenID := map[string]bool{}
	var bindings []envBinding
	var ids []string
	for _, e := range envs {
		name, id, ok := strings.Cut(e, "=")
		if !ok || id == "" {
			return nil, nil, fmt.Errorf("--env %q is not VAR=<ID>", e)
		}
		if !envVarName.MatchString(name) {
			return nil, nil, fmt.Errorf("--env %q: %q is not a valid variable name", e, name)
		}
		if !revealableIDShape(id) {
			return nil, nil, fmt.Errorf("--env %q: %q is not a secret id (sec- or rc- followed by 12 lowercase hex digits)", e, id)
		}
		if seenVar[name] {
			return nil, nil, fmt.Errorf("--env: %s is given more than once", name)
		}
		seenVar[name] = true
		bindings = append(bindings, envBinding{name, id})
		if !seenID[id] {
			seenID[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > secretRevealMaxIDs {
		return nil, nil, fmt.Errorf("%d distinct ids; at most %d per run", len(ids), secretRevealMaxIDs)
	}
	return bindings, ids, nil
}

func runSecretCommand(client httpClient, cfg Config, envs []string, stdinJSONID string, cmdArgs, environ []string,
	in io.Reader, out, errOut io.Writer) int {
	var bindings []envBinding
	var ids []string
	if len(envs) > 0 || stdinJSONID == "" {
		var err error
		if bindings, ids, err = parseEnvBindings(envs); err != nil {
			fmt.Fprintf(errOut, "[ocagent] secret run: %v\n", err)
			return 2
		}
	}
	if stdinJSONID != "" {
		if !revealableIDShape(stdinJSONID) {
			fmt.Fprintf(errOut, "[ocagent] secret run: --stdin-json %q is not a secret id (sec- or rc- followed by 12 lowercase hex digits)\n", stdinJSONID)
			return 2
		}
		if !slices.Contains(ids, stdinJSONID) {
			ids = append(ids, stdinJSONID)
		}
		if len(ids) > secretRevealMaxIDs {
			fmt.Fprintf(errOut, "[ocagent] secret run: %d distinct ids; at most %d per run\n", len(ids), secretRevealMaxIDs)
			return 2
		}
	}
	if len(cmdArgs) == 0 {
		fmt.Fprintln(errOut, "[ocagent] secret run: no command given after --")
		return 2
	}
	if !secretPreflight(cfg, "secret run", errOut) {
		return 3
	}
	// Before the reveal: a retrieval can destroy a card's value, and a command
	// that was never going to start must not pay for it.
	if _, err := exec.LookPath(cmdArgs[0]); err != nil {
		fmt.Fprintf(errOut, "[ocagent] secret run: cannot start %s: %v\n", cmdArgs[0], err)
		return 1
	}
	values, rerr := revealSecrets(client, cfg, ids)
	if rerr != nil {
		fmt.Fprintf(errOut, "[ocagent] secret run: %s\n", rerr.msg)
		return rerr.code
	}

	env := append([]string{}, environ...)
	maskValues := make([]string, 0, len(values))
	for _, b := range bindings {
		env = append(env, b.name+"="+values[b.id])
	}
	for _, v := range values {
		maskValues = append(maskValues, v)
	}
	stdin := in
	if stdinJSONID != "" {
		encoded, forms, err := encodeSecretForStdin(values[stdinJSONID])
		if err != nil {
			// The error names a type, never the value.
			fmt.Fprintf(errOut, "[ocagent] secret run: cannot encode the value: %v\n", err)
			return 1
		}
		stdin = bytes.NewReader(encoded)
		maskValues = append(maskValues, forms...)
	}

	stdout := newMaskWriter(out, maskValues)
	stderr := newMaskWriter(errOut, maskValues)
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = secretRunWaitDelay
	if err := cmd.Start(); err != nil {
		// exec's error names the program and the reason, never the environment.
		fmt.Fprintf(errOut, "[ocagent] secret run: cannot start %s: %v\n", cmdArgs[0], err)
		return 1
	}

	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-sigs:
				_ = cmd.Process.Signal(s)
			case <-done:
				return
			}
		}
	}()
	waitErr := cmd.Wait()
	signal.Stop(sigs)
	close(done)
	stdout.Flush()
	stderr.Flush()

	if waitErr == nil || errors.Is(waitErr, exec.ErrWaitDelay) {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return exitErr.ExitCode()
	}
	fmt.Fprintf(errOut, "[ocagent] secret run: waiting for %s failed: %v\n", cmdArgs[0], waitErr)
	return 1
}

// encodeSecretForStdin returns the JSON string the command reads, and the forms
// common JSON libraries print it back in, because those forms do not hold the
// stored bytes once the value needs escaping: the stdin form, Go's HTML-escaped
// form and Python's default ASCII-only form, each also for the trimmed value (a
// command reading a key file usually strips it first). Masking stays best effort:
// a command that encodes the value any other way prints it.
func encodeSecretForStdin(value string) ([]byte, []string, error) {
	encoded, err := jsonWithoutHTMLEscape(value)
	if err != nil {
		return nil, nil, err
	}
	var forms []string
	for _, v := range []string{value, strings.TrimSpace(value)} {
		plain, err := jsonWithoutHTMLEscape(v)
		if err != nil {
			return nil, nil, err
		}
		htmlEscaped, err := json.Marshal(v)
		if err != nil {
			return nil, nil, err
		}
		for _, quoted := range []string{plain, string(htmlEscaped), asciiOnlyJSON(plain)} {
			forms = append(forms, quoted, quoted[1:len(quoted)-1])
		}
	}
	return []byte(encoded), forms, nil
}

func jsonWithoutHTMLEscape(value string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func asciiOnlyJSON(encoded string) string {
	var b strings.Builder
	for _, r := range encoded {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

// secretRunWaitDelay bounds how long run waits, after the command exits, for a
// background process it left behind to release the output pipes.
var secretRunWaitDelay = 3 * time.Second

// maskWriter replaces every occurrence of any value with secretMask. It holds
// back a tail that could still be the start of a value, so a value split across
// two Writes is caught; Flush releases that tail at end of stream.
type maskWriter struct {
	mu     sync.Mutex
	dst    io.Writer
	values [][]byte
	maxLen int
	buf    []byte
}

func newMaskWriter(dst io.Writer, values []string) *maskWriter {
	m := &maskWriter{dst: dst}
	seen := map[string]bool{}
	add := func(v string) {
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		m.values = append(m.values, []byte(v))
		if len(v) > m.maxLen {
			m.maxLen = len(v)
		}
	}
	for _, v := range values {
		// A value pasted with a trailing newline is often printed trimmed (by
		// the child, or by $(...)), and a multi-line value may be printed with
		// CRLF line ends; neither form contains the stored bytes.
		for _, form := range []string{v, strings.TrimSpace(v)} {
			add(form)
			if strings.Contains(form, "\n") {
				add(strings.ReplaceAll(strings.ReplaceAll(form, "\r\n", "\n"), "\n", "\r\n"))
			}
		}
	}
	return m
}

func (m *maskWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	var out bytes.Buffer
	i := m.scan(&out, false)
	m.buf = append(m.buf[:0], m.buf[i:]...)
	if out.Len() > 0 {
		if _, err := m.dst.Write(out.Bytes()); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (m *maskWriter) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out bytes.Buffer
	m.scan(&out, true)
	m.buf = m.buf[:0]
	if out.Len() > 0 {
		_, _ = m.dst.Write(out.Bytes())
	}
}

type span struct{ start, end int }

// scan emits what is settled and returns where the held-back tail starts.
// Overlapping matches of different values are merged into one masked span:
// masking them one at a time would leave the rest of a value whose head
// another value already consumed.
func (m *maskWriter) scan(out *bytes.Buffer, final bool) int {
	cut := len(m.buf)
	if !final {
		cut = m.heldFrom()
	}
	spans := m.mergedMatches()
	emitted := 0
	for _, s := range spans {
		if s.start >= cut {
			break
		}
		// A span reaching into the held tail may still grow with the next
		// Write, so it is held whole.
		if s.end > cut {
			cut = s.start
			break
		}
		out.Write(m.buf[emitted:s.start])
		out.WriteString(secretMask)
		emitted = s.end
	}
	out.Write(m.buf[emitted:cut])
	return cut
}

func (m *maskWriter) mergedMatches() []span {
	var all []span
	for _, v := range m.values {
		for from := 0; from < len(m.buf); {
			j := bytes.Index(m.buf[from:], v)
			if j < 0 {
				break
			}
			all = append(all, span{from + j, from + j + len(v)})
			from += j + 1
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].start < all[j].start })
	var merged []span
	for _, s := range all {
		if n := len(merged); n > 0 && s.start < merged[n-1].end {
			if s.end > merged[n-1].end {
				merged[n-1].end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

// heldFrom returns the earliest offset whose remaining bytes could still be
// the start of a value that the next Write completes. A complete short value
// there is held too: it may be the head of a longer one.
func (m *maskWriter) heldFrom() int {
	from := len(m.buf) - m.maxLen + 1
	if from < 0 {
		from = 0
	}
	for i := from; i < len(m.buf); i++ {
		tail := m.buf[i:]
		for _, v := range m.values {
			if len(tail) < len(v) && bytes.HasPrefix(v, tail) {
				return i
			}
		}
	}
	return len(m.buf)
}

func cmdSecretWrite(client httpClient, cfg Config, id, path string, force bool, out, errOut io.Writer) int {
	dir := filepath.Dir(path)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(errOut, "[ocagent] secret write: directory %s does not exist (it is not created)\n", dir)
		return 1
	}
	if info, err := os.Lstat(path); err == nil {
		if !force {
			fmt.Fprintf(errOut, "[ocagent] secret write: %s already exists; pass --force to replace it\n", path)
			return 6
		}
		if info.IsDir() {
			fmt.Fprintf(errOut, "[ocagent] secret write: %s is a directory\n", path)
			return 1
		}
	}
	if !secretPreflight(cfg, "secret write", errOut) {
		return 3
	}
	// Before the reveal: a retrieval can destroy a card's value, and a directory
	// that was never going to take the file must not pay for it.
	probe, err := os.CreateTemp(dir, ".ocagent-secret-*")
	if err != nil {
		fmt.Fprintf(errOut, "[ocagent] secret write: cannot create a temporary file next to %s: %v\n", path, err)
		return 1
	}
	probe.Close()
	os.Remove(probe.Name())
	values, rerr := revealSecrets(client, cfg, []string{id})
	if rerr != nil {
		fmt.Fprintf(errOut, "[ocagent] secret write: %s\n", rerr.msg)
		return rerr.code
	}
	if err := writeFileAtomic0600(path, []byte(values[id]), force); err != nil {
		fmt.Fprintf(errOut, "[ocagent] secret write: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "已寫入 %s\n", path)
	return 0
}

// Without force the temp file is LINKED into place, which fails if the path
// appeared since the check above instead of silently replacing it.
func writeFileAtomic0600(path string, data []byte, force bool) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".ocagent-secret-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file next to %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot set 0600 on the temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot write the temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("cannot sync the temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close the temporary file: %w", err)
	}
	if force {
		if err := os.Rename(tmpPath, path); err != nil {
			return fmt.Errorf("cannot move the file into place at %s: %w", path, err)
		}
		return nil
	}
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s already exists; pass --force to replace it", path)
		}
		return fmt.Errorf("cannot move the file into place at %s: %w", path, err)
	}
	return nil
}
