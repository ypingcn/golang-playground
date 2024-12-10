// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// TODO(andybons): add logging
// TODO(andybons): restrict memory use

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bradfitz/gomemcache/memcache"
	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"golang.org/x/playground/internal"
	"golang.org/x/playground/sandbox/sandboxtypes"
)

const (
	// Time for 'go build' to download 3rd-party modules and compile.
	maxBuildTime = 10 * time.Second
	maxRunTime   = 5 * time.Second

	// progName is the implicit program name written to the temp
	// dir and used in compiler and vet errors.
	progName = "prog.go"
)

const (
	goBuildTimeoutError = "timeout running go build"
	runTimeoutError     = "timeout running program"
)

// internalErrors are strings found in responses that will not be cached
// due to their non-deterministic nature.
var internalErrors = []string{
	"out of memory",
	"cannot allocate memory",
}

type request struct {
	Body    string
	WithVet bool // whether client supports vet response in a /compile request (Issue 31970)
}

type response struct {
	Errors      string
	Events      []Event
	Status      int
	IsTest      bool
	TestsFailed int

	// VetErrors, if non-empty, contains any vet errors. It is
	// only populated if request.WithVet was true.
	VetErrors string `json:",omitempty"`
	// VetOK reports whether vet ran & passsed. It is only
	// populated if request.WithVet was true. Only one of
	// VetErrors or VetOK can be non-zero.
	VetOK bool `json:",omitempty"`
}

// commandHandler returns an http.HandlerFunc.
// This handler creates a *request, assigning the "Body" field a value
// from the "body" form parameter or from the HTTP request body.
// If there is no cached *response for the combination of cachePrefix and request.Body,
// handler calls cmdFunc and in case of a nil error, stores the value of *response in the cache.
// The handler returned supports Cross-Origin Resource Sharing (CORS) from any domain.
func (s *server) commandHandler(cachePrefix string, cmdFunc func(context.Context, *request) (*response, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cachePrefix := cachePrefix // so we can modify it below
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == "OPTIONS" {
			// This is likely a pre-flight CORS request.
			return
		}

		var req request
		// Until programs that depend on golang.org/x/tools/godoc/static/playground.js
		// are updated to always send JSON, this check is in place.
		if b := r.FormValue("body"); b != "" {
			req.Body = b
			req.WithVet, _ = strconv.ParseBool(r.FormValue("withVet"))
		} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.log.Errorf("error decoding request: %v", err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}

		if req.WithVet {
			cachePrefix += "_vet" // "prog" -> "prog_vet"
		}

		resp := &response{}
		key := cacheKey(cachePrefix, req.Body)
		if err := s.cache.Get(key, resp); err != nil {
			if !errors.Is(err, memcache.ErrCacheMiss) {
				s.log.Errorf("s.cache.Get(%q, &response): %v", key, err)
			}
			resp, err = cmdFunc(r.Context(), &req)
			if err != nil {
				s.log.Errorf("cmdFunc error: %v", err)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				return
			}
			if strings.Contains(resp.Errors, goBuildTimeoutError) || strings.Contains(resp.Errors, runTimeoutError) {
				// TODO(golang.org/issue/38576) - This should be a http.StatusBadRequest,
				// but the UI requires a 200 to parse the response. It's difficult to know
				// if we've timed out because of an error in the code snippet, or instability
				// on the playground itself. Either way, we should try to show the user the
				// partial output of their program.
				s.writeJSONResponse(w, resp, http.StatusOK)
				return
			}
			for _, e := range internalErrors {
				if strings.Contains(resp.Errors, e) {
					s.log.Errorf("cmdFunc compilation error: %q", resp.Errors)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}
			}
			for _, el := range resp.Events {
				if el.Kind != "stderr" {
					continue
				}
				for _, e := range internalErrors {
					if strings.Contains(el.Message, e) {
						s.log.Errorf("cmdFunc runtime error: %q", el.Message)
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
						return
					}
				}
			}
			if err := s.cache.Set(key, resp); err != nil {
				s.log.Errorf("cache.Set(%q, resp): %v", key, err)
			}
		}

		s.writeJSONResponse(w, resp, http.StatusOK)
	}
}

func cacheKey(prefix, body string) string {
	h := sha256.New()
	io.WriteString(h, body)
	return fmt.Sprintf("%s-%s-%x", prefix, runtime.Version(), h.Sum(nil))
}

// isTestFunc tells whether fn has the type of a testing function.
func isTestFunc(fn *ast.FuncDecl) bool {
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 ||
		fn.Type.Params.List == nil ||
		len(fn.Type.Params.List) != 1 ||
		len(fn.Type.Params.List[0].Names) > 1 {
		return false
	}
	ptr, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	// We can't easily check that the type is *testing.T
	// because we don't know how testing has been imported,
	// but at least check that it's *T or *something.T.
	if name, ok := ptr.X.(*ast.Ident); ok && name.Name == "T" {
		return true
	}
	if sel, ok := ptr.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "T" {
		return true
	}
	return false
}

// isTest tells whether name looks like a test (or benchmark, according to prefix).
// It is a Test (say) if there is a character after Test that is not a lower-case letter.
// We don't want mistaken Testimony or erroneous Benchmarking.
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) { // "Test" is ok
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

// getTestProg returns source code that executes all valid tests and examples in src.
// If the main function is present or there are no tests or examples, it returns nil.
// getTestProg emulates the "go test" command as closely as possible.
// Benchmarks are not supported because of sandboxing.
func getTestProg(src []byte) []byte {
	fset := token.NewFileSet()
	// Early bail for most cases.
	f, err := parser.ParseFile(fset, progName, src, parser.ImportsOnly)
	if err != nil || f.Name.Name != "main" {
		return nil
	}

	// importPos stores the position to inject the "testing" import declaration, if needed.
	importPos := fset.Position(f.Name.End()).Offset

	var testingImported bool
	for _, s := range f.Imports {
		if s.Path.Value == `"testing"` && s.Name == nil {
			testingImported = true
			break
		}
	}

	// Parse everything and extract test names.
	f, err = parser.ParseFile(fset, progName, src, parser.ParseComments)
	if err != nil {
		return nil
	}

	var tests []string
	for _, d := range f.Decls {
		n, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := n.Name.Name
		switch {
		case name == "main":
			// main declared as a method will not obstruct creation of our main function.
			if n.Recv == nil {
				return nil
			}
		case isTest(name, "Test") && isTestFunc(n):
			tests = append(tests, name)
		}
	}

	// Tests imply imported "testing" package in the code.
	// If there is no import, bail to let the compiler produce an error.
	if !testingImported && len(tests) > 0 {
		return nil
	}

	// We emulate "go test". An example with no "Output" comment is compiled,
	// but not executed. An example with no text after "Output:" is compiled,
	// executed, and expected to produce no output.
	var ex []*doc.Example
	// exNoOutput indicates whether an example with no output is found.
	// We need to compile the program containing such an example even if there are no
	// other tests or examples.
	exNoOutput := false
	for _, e := range doc.Examples(f) {
		if e.Output != "" || e.EmptyOutput {
			ex = append(ex, e)
		}
		if e.Output == "" && !e.EmptyOutput {
			exNoOutput = true
		}
	}

	if len(tests) == 0 && len(ex) == 0 && !exNoOutput {
		return nil
	}

	if !testingImported && (len(ex) > 0 || exNoOutput) {
		// In case of the program with examples and no "testing" package imported,
		// add import after "package main" without modifying line numbers.
		importDecl := []byte(`;import "testing";`)
		src = bytes.Join([][]byte{src[:importPos], importDecl, src[importPos:]}, nil)
	}

	data := struct {
		Tests    []string
		Examples []*doc.Example
	}{
		tests,
		ex,
	}
	code := new(bytes.Buffer)
	if err := testTmpl.Execute(code, data); err != nil {
		panic(err)
	}
	src = append(src, code.Bytes()...)
	return src
}

var testTmpl = template.Must(template.New("main").Parse(`
func main() {
	matchAll := func(t string, pat string) (bool, error) { return true, nil }
	tests := []testing.InternalTest{
{{range .Tests}}
		{"{{.}}", {{.}}},
{{end}}
	}
	examples := []testing.InternalExample{
{{range .Examples}}
		{"Example{{.Name}}", Example{{.Name}}, {{printf "%q" .Output}}, {{.Unordered}}},
{{end}}
	}
	testing.Main(matchAll, tests, nil, examples)
}
`))

var failedTestPattern = "--- FAIL"

// compileAndRun tries to build and run a user program.
// The output of successfully ran program is returned in *response.Events.
// If a program cannot be built or has timed out,
// *response.Errors contains an explanation for a user.
func compileAndRun(ctx context.Context, req *request) (*response, error) {
	// TODO(andybons): Add semaphore to limit number of running programs at once.
	tmpDir, err := ioutil.TempDir("", "sandbox")
	if err != nil {
		return nil, fmt.Errorf("error creating temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	log.Printf("%s: start sandboxBuild", tmpDir)
	br, err := sandboxBuild(ctx, tmpDir, []byte(req.Body), req.WithVet)
	if err != nil {
		log.Printf("%s: error sandboxBuild: %v", tmpDir, err)
		return nil, err
	}
	if br.errorMessage != "" {
		log.Printf("%s: error sandboxBuild build result: %v", tmpDir, br.errorMessage)
		return &response{Errors: br.errorMessage}, nil
	}

	log.Printf("%s: start sandboxBuild", tmpDir)
	execRes, err := sandboxRun(ctx, br.exePath, br.testParam)
	if err != nil {
		log.Printf("%s: error sandboxRun: %v", tmpDir, err)
		return nil, err
	}
	if execRes.Error != "" {
		return &response{Errors: execRes.Error}, nil
	}

	rec := new(Recorder)
	rec.Stdout().Write(execRes.Stdout)
	rec.Stderr().Write(execRes.Stderr)
	events, err := rec.Events()
	if err != nil {
		log.Printf("error decoding events: %v", err)
		return nil, fmt.Errorf("error decoding events: %v", err)
	}
	var fails int
	if br.testParam != "" {
		// In case of testing the TestsFailed field contains how many tests have failed.
		for _, e := range events {
			fails += strings.Count(e.Message, failedTestPattern)
		}
	}
	return &response{
		Events:      events,
		Status:      execRes.ExitCode,
		IsTest:      br.testParam != "",
		TestsFailed: fails,
		VetErrors:   br.vetOut,
		VetOK:       req.WithVet && br.vetOut == "",
	}, nil
}

// buildResult is the output of a sandbox build attempt.
type buildResult struct {
	// goPath is a temporary directory if the binary was built with module support.
	// TODO(golang.org/issue/25224) - Why is the module mode built so differently?
	goPath string
	// exePath is the path to the built binary.
	exePath string
	// testParam is set if tests should be run when running the binary.
	testParam string
	// errorMessage is an error message string to be returned to the user.
	errorMessage string
	// vetOut is the output of go vet, if requested.
	vetOut string
}

// cleanup cleans up the temporary goPath created when building with module support.
func (b *buildResult) cleanup() error {
	if b.goPath != "" {
		return os.RemoveAll(b.goPath)
	}
	return nil
}

// sandboxBuild builds a Go program and returns a build result that includes the build context.
//
// An error is returned if a non-user-correctable error has occurred.
func sandboxBuild(ctx context.Context, tmpDir string, in []byte, vet bool) (br *buildResult, err error) {
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "error"
		}
		// Ignore error. The only error can be invalid tag key or value
		// length, which we know are safe.
		stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(kGoBuildSuccess, status)},
			mGoBuildLatency.M(float64(time.Since(start))/float64(time.Millisecond)))
	}()

	files, err := splitFiles(in)
	if err != nil {
		return &buildResult{errorMessage: err.Error()}, nil
	}

	br = new(buildResult)
	defer br.cleanup()
	var buildPkgArg = "."
	if files.Num() == 1 && len(files.Data(progName)) > 0 {
		buildPkgArg = progName
		src := files.Data(progName)
		if code := getTestProg(src); code != nil {
			br.testParam = "-test.v"
			files.AddFile(progName, code)
		}
	}

	if !files.Contains("go.mod") {
		files.AddFile("go.mod", []byte("module play\n"))
	}

	for f, src := range files.m {
		// Before multi-file support we required that the
		// program be in package main, so continue to do that
		// for now. But permit anything in subdirectories to have other
		// packages.
		if !strings.Contains(f, "/") {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, f, src, parser.PackageClauseOnly)
			if err == nil && f.Name.Name != "main" {
				return &buildResult{errorMessage: "package name must be main"}, nil
			}
		}

		in := filepath.Join(tmpDir, f)
		if strings.Contains(f, "/") {
			if err := os.MkdirAll(filepath.Dir(in), 0755); err != nil {
				return nil, err
			}
		}
		if err := ioutil.WriteFile(in, src, 0644); err != nil {
			return nil, fmt.Errorf("error creating temp file %q: %v", in, err)
		}
	}

	br.exePath = filepath.Join(tmpDir, "a.out")
	goCache := filepath.Join(tmpDir, "gocache")

	cmd := exec.Command("/usr/local/go-faketime/bin/go", "build", "-o", br.exePath, "-tags=faketime")
	cmd.Dir = tmpDir
	cmd.Env = []string{"GOOS=linux", "GOARCH=amd64", "GOROOT=/usr/local/go-faketime"}
	cmd.Env = append(cmd.Env, "GOCACHE="+goCache)
	cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
	if os.Getenv("GOPRIVATE") != "" || os.Getenv("GONOPROXY") != "" || os.Getenv("GONOSUMDB") != "" {
		cmd.Env = append(cmd.Env, "GOPRIVATE="+os.Getenv("GOPRIVATE"))
		cmd.Env = append(cmd.Env, "GONOPROXY="+os.Getenv("GONOPROXY"))
		cmd.Env = append(cmd.Env, "GONOSUMDB="+os.Getenv("GONOSUMDB"))
	}
	// Create a GOPATH just for modules to be downloaded
	// into GOPATH/pkg/mod.
	cmd.Args = append(cmd.Args, "-modcacherw")
	cmd.Args = append(cmd.Args, "-mod=mod")
	br.goPath, err = ioutil.TempDir("", "gopath-")
	if err != nil {
		log.Printf("error creating temp directory: %v", err)
		return nil, fmt.Errorf("error creating temp directory: %v", err)
	}
	cmd.Env = append(cmd.Env, "GO111MODULE=on", "GOPROXY="+playgroundGoproxy())
	cmd.Args = append(cmd.Args, buildPkgArg)
	cmd.Env = append(cmd.Env, "GOPATH="+br.goPath)
	out := &bytes.Buffer{}
	cmd.Stderr, cmd.Stdout = out, out

	log.Printf("Command ==> %v", cmd.String())
	log.Printf("Env     ==> %v", cmd.Env)

	if err := cmd.Start(); err != nil {
		log.Printf("error starting go build: %v", err)
		return nil, fmt.Errorf("error starting go build: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, maxBuildTime)
	defer cancel()
	if err := internal.WaitOrStop(ctx, cmd, os.Interrupt, 250*time.Millisecond); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			br.errorMessage = fmt.Sprintln(goBuildTimeoutError)
		} else if ee := (*exec.ExitError)(nil); !errors.As(err, &ee) {
			log.Printf("error building go source: %v", err)
			return nil, fmt.Errorf("error building go source: %v", err)
		}
		// Return compile errors to the user.
		// Rewrite compiler errors to strip the tmpDir name.
		br.errorMessage = br.errorMessage + strings.Replace(string(out.Bytes()), tmpDir+"/", "", -1)

		// "go build", invoked with a file name, puts this odd
		// message before any compile errors; strip it.
		br.errorMessage = strings.Replace(br.errorMessage, "# command-line-arguments\n", "", 1)

		return br, nil
	}
	const maxBinarySize = 100 << 20 // copied from sandbox backend; TODO: unify?
	if fi, err := os.Stat(br.exePath); err != nil || fi.Size() == 0 || fi.Size() > maxBinarySize {
		if err != nil {
			log.Printf("failed to stat binary: %v", err)
			return nil, fmt.Errorf("failed to stat binary: %v", err)
		}
		log.Printf("invalid binary size %d", fi.Size())
		return nil, fmt.Errorf("invalid binary size %d", fi.Size())
	}
	if vet {
		// TODO: do this concurrently with the execution to reduce latency.
		br.vetOut, err = vetCheckInDir(ctx, tmpDir, br.goPath)
		if err != nil {
			log.Printf("running vet: %v", err)
			return nil, fmt.Errorf("running vet: %v", err)
		}
	}
	return br, nil
}

// sandboxRun runs a Go binary in a sandbox environment.
func sandboxRun(ctx context.Context, exePath string, testParam string) (execRes sandboxtypes.Response, err error) {
	start := time.Now()
	defer func() {
		status := "success"
		if err != nil {
			status = "error"
		}
		// Ignore error. The only error can be invalid tag key or value
		// length, which we know are safe.
		stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(kGoBuildSuccess, status)},
			mGoRunLatency.M(float64(time.Since(start))/float64(time.Millisecond)))
	}()
	exeBytes, err := ioutil.ReadFile(exePath)
	if err != nil {
		return execRes, err
	}
	ctx, cancel := context.WithTimeout(ctx, maxRunTime)
	defer cancel()
	sreq, err := http.NewRequestWithContext(ctx, "POST", sandboxBackendURL(), bytes.NewReader(exeBytes))
	if err != nil {
		return execRes, fmt.Errorf("NewRequestWithContext %q: %w", sandboxBackendURL(), err)
	}
	sreq.Header.Add("Idempotency-Key", "1") // lets Transport do retries with a POST
	if testParam != "" {
		sreq.Header.Add("X-Argument", testParam)
	}
	sreq.GetBody = func() (io.ReadCloser, error) { return ioutil.NopCloser(bytes.NewReader(exeBytes)), nil }
	res, err := sandboxBackendClient().Do(sreq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			execRes.Error = runTimeoutError
			return execRes, nil
		}
		return execRes, fmt.Errorf("POST %q: %w", sandboxBackendURL(), err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		log.Printf("unexpected response from backend: %v", res.Status)
		return execRes, fmt.Errorf("unexpected response from backend: %v", res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(&execRes); err != nil {
		log.Printf("JSON decode error from backend: %v", err)
		return execRes, errors.New("error parsing JSON from backend")
	}
	return execRes, nil
}

// playgroundGoproxy returns the GOPROXY environment config the playground should use.
// It is fetched from the environment variable PLAY_GOPROXY. A missing or empty
// value for PLAY_GOPROXY returns the default value of https://proxy.golang.org.
func playgroundGoproxy() string {
	proxypath := os.Getenv("PLAY_GOPROXY")
	if proxypath != "" {
		return proxypath
	}
	// return "https://proxy.golang.org"
	return "https://goproxy.cn"
}

// healthCheck attempts to build a binary from the source in healthProg.
// It returns any error returned from sandboxBuild, or nil if none is returned.
func (s *server) healthCheck(ctx context.Context) error {
	tmpDir, err := ioutil.TempDir("", "sandbox")
	if err != nil {
		return fmt.Errorf("error creating temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	br, err := sandboxBuild(ctx, tmpDir, []byte(healthProg), false)
	if err != nil {
		return err
	}
	if br.errorMessage != "" {
		return errors.New(br.errorMessage)
	}
	return nil
}

// sandboxBackendURL returns the URL of the sandbox backend that
// executes binaries. This backend is required for Go 1.14+ (where it
// executes using gvisor, since Native Client support is removed).
//
// This function either returns a non-empty string or it panics.
func sandboxBackendURL() string {
	if v := os.Getenv("SANDBOX_BACKEND_URL"); v != "" {
		return v
	}
	panic("need set SANDBOX_BACKEND_URL environment")
}

var sandboxBackendOnce struct {
	sync.Once
	c *http.Client
}

func sandboxBackendClient() *http.Client {
	sandboxBackendOnce.Do(initSandboxBackendClient)
	return sandboxBackendOnce.c
}

// initSandboxBackendClient runs from a sync.Once and initializes
// sandboxBackendOnce.c with the *http.Client we'll use to contact the
// sandbox execution backend.
func initSandboxBackendClient() {
	sandboxBackendOnce.c = http.DefaultClient
}

const healthProg = `
package main

import "fmt"

func main() { fmt.Print("ok") }
`
