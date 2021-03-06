package main

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestCmdExec(t *testing.T) {
	appFs = afero.NewMemMapFs()

	outputPath := "output.log"
	log = logger{
		cmdLogger: fileLogger(outputPath),
	}

	expStdout := "standard out"
	expStderr := "standard error"
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("echo %s ; echo %s 1>&2", expStdout, expStderr))
	stdout, stderr, err := execCmd(cmd, "PREFIX")

	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
		return
	}
	if stdout != expStdout {
		t.Errorf("Stdout didn't match: expected %s, got %s", expStdout, stdout)
	}
	if stderr != expStderr {
		t.Errorf("Stderr didn't match: expected %s, got %s", expStderr, stderr)
	}
}

func TestWaitFor(t *testing.T) {
	sleep = func(t time.Duration) {}

	calls := 0
	callThreeTimes := func() bool {
		calls++
		if calls == 3 {
			return true
		}
		return false
	}
	err := waitFor(callThreeTimes, 5)
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
	}
	if calls != 3 {
		t.Errorf("Incorrect number of calls to predicate: %d", calls)
	}

	err = waitFor(func() bool {
		return false
	}, 300*time.Millisecond)
	if err.Error() != "timed out" {
		t.Errorf("Expected waitFor to timeout")
	}
}

func TestURLGeneration(t *testing.T) {
	t.Parallel()

	l := logger{
		rootDir: "/var/www/quilt-tester/test",
		ip:      "8.8.8.8",
	}
	res := l.url()
	exp := "http://8.8.8.8/test"
	if res != exp {
		t.Errorf("Bad URL generation, expected %s, got %s.", exp, res)
	}
}

func TestUpdateNamespace(t *testing.T) {
	appFs = afero.NewMemMapFs()

	specPath := "/test.spec"
	err := overwrite(specPath, `require("spark");
var deployment = createDeployment({namespace: "replace"});
deployment.deploy(new Machine({}));`)
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
	}
	updateNamespace(specPath, "test-namespace")

	res, err := fileContents(specPath)
	exp := `require("spark");
var deployment = createDeployment({namespace: "replace"});
deployment.deploy(new Machine({}));; deployment.namespace = "test-namespace";`
	if err != nil {
		t.Errorf("Unexpected error: %s", err.Error())
		return
	}
	if res != exp {
		t.Errorf("Namespace didn't properly update, expected %s, got %s",
			exp, res)
	}
}
