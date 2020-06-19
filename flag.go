package main

import (
	"fmt"
	"os"
	"os/exec"
)

type usageErr struct {
	err error
}

func (u usageErr) Error() string { return u.err.Error() }

func main() { os.Exit(main1()) }

func main1() int {
	err := mainerr()
	if err == nil {
		return 0
	}
	switch err := err.(type) {
	case *exec.ExitError:
		return err.ExitCode()
	case usageErr:
		fmt.Fprint(os.Stderr, err.Error())
		return 2
	}
	fmt.Fprintln(os.Stderr, err)
	return 1
}
