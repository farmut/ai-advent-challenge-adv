// Command sbx-run is the entry point of every sandboxed command. It runs inside
// the sandbox container as an unprivileged user and:
//
//  1. copies the read-only task directory (/src) into the writable tmpfs
//     (/tmp/work), skipping symlinks and refusing oversized trees;
//  2. checks that the task uses only the dependencies baked into the image
//     (Go: every go.mod requirement is in the read-only module cache; Vue:
//     package.json dependencies equal the template's) — there is no network,
//     so anything else would fail later with a far less clear message;
//  3. for the go stack links the prebuilt build cache of the template's
//     dependencies into the writable GOCACHE (gin is not recompiled on every
//     run); for the vue stack builds a writable node_modules in the tmpfs whose
//     entries are symlinks into the read-only preinstalled tree (tools such as
//     vite write temp files into node_modules);
//  4. replaces itself (execve) with the target argv in /tmp/work.
//
// It uses only the standard library and never invokes a shell.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

// Exit codes reserved by sbx-run. Anything else is the target's own code.
const (
	exitUsage = 96 // bad invocation or internal failure
	exitDeps  = 97 // dependencies not from the template
	exitCopy  = 98 // task tree over the copy limits
)

// allowedBinaries mirrors the host runner's allowlist: a second, independent
// gate inside the container.
var allowedBinaries = map[string]bool{
	"go": true, "gofmt": true, "gosec": true,
	"eslint": true, "vitest": true, "vue-tsc": true, "npm": true,
}

type options struct {
	stack       string
	src, work   string
	templates   string
	modCache    string
	vueModules  string
	cacheSeed   string
	goCache     string
	maxFiles    int
	maxBytes    int64
	argv        []string
	stderr      io.Writer
	lookPath    func(string) (string, error)
	execProcess func(path string, argv, env []string) error
}

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "sbx-run: %v\n", err)
		os.Exit(exitUsage)
	}
	o.stderr = os.Stderr
	o.lookPath = exec.LookPath
	o.execProcess = syscall.Exec
	os.Exit(run(o))
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("sbx-run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	o := options{}
	fs.StringVar(&o.stack, "stack", "", "go | vue")
	fs.StringVar(&o.src, "src", "/src", "read-only task directory")
	fs.StringVar(&o.work, "work", "/tmp/work", "writable working copy")
	fs.StringVar(&o.templates, "templates", "/opt/sandbox/templates", "dependency templates")
	fs.StringVar(&o.vueModules, "node-modules", "/opt/vue/node_modules", "preinstalled node_modules")
	fs.StringVar(&o.cacheSeed, "gocache-seed", "/opt/gocache", "read-only prebuilt GOCACHE (template deps)")
	fs.IntVar(&o.maxFiles, "max-files", 5000, "copy limit: files")
	fs.Int64Var(&o.maxBytes, "max-bytes", 50<<20, "copy limit: bytes")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	o.argv = fs.Args()
	switch o.stack {
	case "go", "vue":
	default:
		return o, fmt.Errorf("--stack must be go or vue, got %q", o.stack)
	}
	if len(o.argv) == 0 {
		return o, errors.New("no command after --")
	}
	if o.maxFiles <= 0 || o.maxBytes <= 0 {
		return o, errors.New("--max-files and --max-bytes must be > 0")
	}
	o.goCache = os.Getenv("GOCACHE")
	o.modCache = os.Getenv("GOMODCACHE")
	if o.modCache == "" {
		o.modCache = "/opt/gomodcache"
	}
	return o, nil
}

// run performs the preparation and the exec. It returns an exit code only
// when the exec did not happen.
func run(o options) int {
	if !allowedBinaries[o.argv[0]] {
		fmt.Fprintf(o.stderr, "sbx-run: команда %q не разрешена в песочнице\n", o.argv[0])
		return exitUsage
	}
	if err := copyTree(o.src, o.work, o.maxFiles, o.maxBytes); err != nil {
		fmt.Fprintf(o.stderr, "sbx-run: %v\n", err)
		if errors.Is(err, errLimit) {
			return exitCopy
		}
		return exitUsage
	}
	switch o.stack {
	case "go":
		if err := checkGoDeps(o.work, o.modCache); err != nil {
			fmt.Fprintf(o.stderr, "sbx-run: %v\n", err)
			return exitDeps
		}
		if err := seedGoCache(o.cacheSeed, o.goCache); err != nil {
			fmt.Fprintf(o.stderr, "sbx-run: %v\n", err)
			return exitUsage
		}
	case "vue":
		if err := checkVueDeps(o.work, o.templates+"/vue/package.json"); err != nil {
			fmt.Fprintf(o.stderr, "sbx-run: %v\n", err)
			return exitDeps
		}
		if err := linkNodeModules(o.vueModules, o.work+"/node_modules"); err != nil {
			fmt.Fprintf(o.stderr, "sbx-run: link node_modules: %v\n", err)
			return exitUsage
		}
	}
	path, err := o.lookPath(o.argv[0])
	if err != nil {
		fmt.Fprintf(o.stderr, "sbx-run: %q не найден в образе: %v\n", o.argv[0], err)
		return 127
	}
	if err := os.Chdir(o.work); err != nil {
		fmt.Fprintf(o.stderr, "sbx-run: chdir: %v\n", err)
		return exitUsage
	}
	err = o.execProcess(path, o.argv, os.Environ())
	fmt.Fprintf(o.stderr, "sbx-run: exec %s: %v\n", path, err)
	return 126
}
