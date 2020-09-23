// The `docker-compose` command is a wrapper around the real docker-compose
// that provides compose-file relative resolution and composition of the
// `COMPOSE_FILE` environment variable  and `-f` flag values.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/myitcv/docker-compose/internal/os/execpath"
	"golang.org/x/sync/errgroup"
)

const (
	envComposeResolve = "COMPOSE_RESOLVE"
)

var (
	debug = os.Getenv("COMPOSE_RESOLVE_DEBUG") != ""
)

type fileValue struct {
	files *[]string
}

func (f fileValue) String() string {
	if f.files == nil {
		return ""
	}
	return strings.Join(*f.files, " ")
}

func (f fileValue) Set(v string) error {
	*f.files = append(*f.files, v)
	return nil
}

func mainerr() (err error) {
	fs := flag.NewFlagSet("docker-compose", flag.ContinueOnError)

	var fHelp bool
	fs.BoolVar(&fHelp, "h", false, "Help information")
	fs.BoolVar(&fHelp, "help", false, "Help information")

	var files []string
	fs.Var(fileValue{&files}, "f", "Specify an alternate compose file")
	fs.Var(fileValue{&files}, "file", "Specify an alternate compose file")

	var fProjectName string
	fs.StringVar(&fProjectName, "p", "", "Specify an alternate project name")
	fs.StringVar(&fProjectName, "project-name", "", "Specify an alternate project name")

	var fContext string
	fs.StringVar(&fContext, "c", "", "Specify a context name")
	fs.StringVar(&fContext, "context", "", "Specify a context name")

	var fVerbose bool
	fs.BoolVar(&fVerbose, "verbose", false, "Show more output")

	var fLogLevel string
	fs.StringVar(&fLogLevel, "log-level", "", "Set log level (DEBUG, INFO, WARNING, ERROR, CRITICAL)")

	var fNoAnsi bool
	fs.BoolVar(&fNoAnsi, "no-ansi", false, "Do not print ANSI control characters")

	var fVersion bool
	fs.BoolVar(&fVersion, "v", false, "Print version and exit")
	fs.BoolVar(&fVersion, "version", false, "Print version and exit")

	var fHost string
	fs.StringVar(&fHost, "H", "", "Daemon socket to connect to")
	fs.StringVar(&fHost, "host", "", "Daemon socket to connect to")

	// TODO: TLS-related flags

	var fProjectDir string
	fs.StringVar(&fProjectDir, "project-directory", "", "Specify an alternate working directory")

	var fCompatibility bool
	fs.BoolVar(&fCompatibility, "compatibility", false, "If set, Compose will attempt to convert keys in v3 files to their non-Swarm equivalent")

	var fEnvFile string
	fs.StringVar(&fEnvFile, "env-file", "", "Specify an alternate environment file")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	isResolver := os.Getenv(envComposeResolve) == ""

	// Find the "underlying" docker-compose in PATH (which also has the side
	// effect of updating COMPOSE_RESOLVE with self)
	dc, err := resolveDockerCompose()
	if err != nil {
		return err
	}

	args := os.Args[1:]

	if isResolver {
		td, files, err := resolveComposeFiles(dc, files)
		if err != nil {
			return fmt.Errorf("failed to resolve docker-compose file args: %v", err)
		}
		// Non-nil error - we have to tidy up the files
		defer func() {
			if td != "" {
				os.RemoveAll(td)
			}
		}()
		args = nil
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "f" || f.Name == "file" {
				return
			}
			args = append(args, fmt.Sprintf("-%v=%v", f.Name, f.Value.String()))
		})
		for _, f := range files {
			args = append(args, "-f", f)
		}
		args = append(args, fs.Args()...)
	}

	cmd := exec.Command(dc, args...)
	debugf("call: %v\n", strings.Join(cmd.Args, " "))
	cmd.Env = append(os.Environ(), "COMPOSE_FILE=")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func resolveDockerCompose() (string, error) {
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		return "", fmt.Errorf("failed to resolve os.Args[0] to absolute path: %v", err)
	}
	prev := strings.Split(os.Getenv(envComposeResolve), string(os.PathListSeparator))
	prev = append(prev, self)

	selfDir, err := filepath.Abs(filepath.Dir(self))
	if err != nil {
		return "", err
	}
	path := os.Getenv("PATH")
	pathElems := filepath.SplitList(path)
	for len(pathElems) > 0 {
		searchPath := strings.Join(pathElems, string(os.PathListSeparator))
		which, err := execpath.Look("docker-compose", func(v string) string {
			if v == "PATH" {
				return searchPath
			}
			return os.Getenv(v)
		})
		if err != nil {
			return "", fmt.Errorf("failed to try and resolve docker-compose from path %q: %v", searchPath, err)
		}
		absWhich, err := filepath.Abs(which)
		if err != nil {
			return "", fmt.Errorf("failed to make %q absolute: %v", which, err)
		}
		for _, p := range prev {
			if p == absWhich {
				goto NextPathEntry
			}
		}
		os.Setenv(envComposeResolve, strings.Join(prev, string(os.PathListSeparator)))
		return absWhich, nil
	NextPathEntry:
		// We found a previous instance of ourselves; search the remainder of the
		// path elems by dropping all elements up to an including the directory
		// containing the resolved docker-compose
		for i, p := range pathElems {
			ap, err := filepath.Abs(p)
			if err != nil {
				return "", fmt.Errorf("failed to convert %q to an absolute path: %v", p, err)
			}
			if ap == selfDir {
				pathElems = pathElems[i+1:]
			}
		}
	}
	return "", fmt.Errorf("failed to find docker-compose in PATH %q", path)
}

func resolveComposeFiles(dc string, files []string) (string, []string, error) {
	// Make a copy of the files slice, noting that they might
	// be dupes, particular when we take into account the
	// value of COMPOSE_FILE below. Last file wins when it
	// comes to dupes.
	dupFiles := append([]string{}, files...)

	// If we return a non-nil error, we should be responsible for any cleanup A
	// value of td != "" indicates there is cleanup we are responsible for to do
	var td string
	defer func() {
		if td != "" {
			os.RemoveAll(td)
		}
	}()

	var envFiles []string
	for _, f := range strings.Split(os.Getenv("COMPOSE_FILE"), string(os.PathListSeparator)) {
		f = strings.TrimSpace(f)
		if f != "" {
			envFiles = append(envFiles, f)
		}
	}
	dupFiles = append(envFiles, dupFiles...)
	for i, f := range dupFiles {
		abs, err := filepath.Abs(f)
		if err != nil {
			return "", nil, fmt.Errorf("failed to make %q absolute: %v", f, err)
		}
		dupFiles[i] = abs
	}

	seen := make(map[string]bool)
	var compFiles []string
	for i := len(dupFiles) - 1; i >= 0; i-- {
		f := dupFiles[i]
		if seen[f] {
			continue
		}
		seen[f] = true
		compFiles = append(compFiles, f)
	}
	for i := 0; i < len(compFiles)/2; i++ {
		compFiles[i], compFiles[len(compFiles)-1-i] = compFiles[len(compFiles)-1-i], compFiles[i]
	}

	td, err := ioutil.TempDir("", "")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %v", err)
	}
	var res []string
	var eg errgroup.Group
	for _, file := range compFiles {
		dir := filepath.Dir(file)
		// TODO: there is a potential optimisation here where runs of files that
		// are in the same directory can have their config resolved with a single
		// command instead of one command per file. Leave this for now
		tf, err := ioutil.TempFile(td, "")
		if err != nil {
			return "", nil, fmt.Errorf("failed to create temp output file in %v: %v", td, err)
		}
		res = append(res, tf.Name())
		cmd := exec.Command(dc, "-f", file, "config")
		debugf("resolve: %v\n", strings.Join(cmd.Args, " "))
		var stderr bytes.Buffer
		cmd.Env = append(os.Environ(), "PWD="+dir)
		cmd.Stdout = tf
		cmd.Stderr = &stderr
		cmd.Dir = dir
		eg.Go(func() error {
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to run [%v] in %v: %v\n%s", strings.Join(cmd.Args, " "), dir, err, stderr.Bytes())
			}
			return tf.Close()
		})
	}

	if err := eg.Wait(); err != nil {
		return "", nil, err
	}

	toRemove := td
	td = ""

	return toRemove, res, nil
}

func debugf(format string, args ...interface{}) {
	if debug {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}
