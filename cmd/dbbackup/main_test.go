package main

import "testing"

func TestDispatchUnknownCommand(t *testing.T) {
	code := dispatch([]string{"bogus"})
	if code != 2 {
		t.Fatalf("dispatch(bogus) = %d, want 2", code)
	}
}

func TestDispatchNoArgsDefaultsToRun(t *testing.T) {
	if got := commandName(nil); got != "run" {
		t.Fatalf("commandName(nil) = %q, want \"run\"", got)
	}
}

func TestAllCommandsRegistered(t *testing.T) {
	for _, name := range []string{"run", "healthcheck", "backup", "validate", "migrate"} {
		if _, ok := commands[name]; !ok {
			t.Errorf("command %q is not registered", name)
		}
	}
}
