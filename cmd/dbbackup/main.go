// Command dbbackup schedules and runs database backups.
package main

import (
	"fmt"
	"os"
)

// commands maps a subcommand name to its implementation. Each command
// receives its own args (without the command name) and returns an exit code.
var commands = map[string]func(args []string) int{}

func commandName(args []string) string {
	if len(args) == 0 {
		return "run"
	}
	return args[0]
}

func dispatch(args []string) int {
	name := commandName(args)
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q (expected run|healthcheck|backup|validate|migrate)\n", name)
		return 2
	}
	rest := args
	if len(rest) > 0 {
		rest = rest[1:]
	}
	return cmd(rest)
}

func main() {
	os.Exit(dispatch(os.Args[1:]))
}
