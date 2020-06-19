package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/gotooltest"
	"github.com/rogpeppe/go-internal/testscript"
)

var (
	fUpdate = flag.Bool("update", false, "update testscripts in place")
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"docker-compose": main1,
	}))
}

func TestScripts(t *testing.T) {
	// For the debug test we need to install a version of ourselves to two temp
	// directories and add those temp directories to the PATH in order that we
	// effectively call ourselves (albeit with the COMPOSE_RESOLVE variable set)
	// We do this twice two ensure that we have the correct logic for tracking
	// the previous versions of self that we have run
	td1 := installSelf(t)
	td2 := installSelf(t)

	path := []string{td1, td2, os.Getenv("PATH")}

	p := testscript.Params{
		Setup: func(env *testscript.Env) error {
			env.Vars = append(env.Vars,
				"PATH="+strings.Join(path, string(os.PathListSeparator)),
				"TD1="+td1,
				"TD2="+td2,
			)
			return nil
		},
		Dir:           "testdata",
		UpdateScripts: *fUpdate,
	}
	if err := gotooltest.Setup(&p); err != nil {
		t.Fatal(err)
	}
	testscript.Run(t, p)
}

func installSelf(t *testing.T) string {
	t.Helper()
	td, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(td)
	})

	cmd := exec.Command("go", "install")
	cmd.Env = append(os.Environ(), "GOBIN="+td)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run [%v]: %v\n%s", cmd, err, out)
	}
	return td
}
