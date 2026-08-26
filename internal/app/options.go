package app

import (
	"fmt"
	"strings"
)

// Command identifies a headless workflow operation.
type Command string

const (
	CommandNext    Command = "next"
	CommandPrev    Command = "prev"
	CommandSet     Command = "set"
	CommandComment Command = "comment"
	CommandSync    Command = "sync"
	CommandStatus  Command = "status"
)

// Options is the parsed command line shared by all backends.
type Options struct {
	Command  Command
	Status   string
	Comment  string
	Target   string
	Socket   string
	StateKey string
	Config   string
	Help     bool
}

// ParseOptions parses backend-independent headless CLI arguments.
func ParseOptions(args []string) (Options, error) {
	var options Options
	var positional []string
	var helpRequested bool
	parsingOptions := true

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !parsingOptions {
			positional = append(positional, arg)
			continue
		}
		if arg == "--" {
			parsingOptions = false
			continue
		}
		if arg == "-h" || arg == "--help" {
			helpRequested = true
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}

		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch name {
		case "target", "socket", "state-key", "config":
		default:
			return Options{}, fmt.Errorf("unknown flag --%s", name)
		}
		if !hasValue {
			i++
			if i >= len(args) || strings.HasPrefix(args[i], "--") || args[i] == "-h" {
				return Options{}, fmt.Errorf("missing value for --%s", name)
			}
			value = args[i]
		}
		if value == "" {
			return Options{}, fmt.Errorf("missing value for --%s", name)
		}
		switch name {
		case "target":
			options.Target = value
		case "socket":
			options.Socket = value
		case "state-key":
			options.StateKey = value
		case "config":
			options.Config = value
		}
	}

	if len(positional) == 0 {
		options.Help = true
		return options, nil
	}
	options.Command = Command(positional[0])
	arguments := positional[1:]
	if options.Command == "help" {
		if len(arguments) != 0 {
			return Options{}, fmt.Errorf("help takes no arguments")
		}
		options.Help = true
		return options, nil
	}
	switch options.Command {
	case CommandNext, CommandPrev, CommandSync, CommandStatus:
		if len(arguments) != 0 {
			return Options{}, fmt.Errorf("%s takes no arguments", options.Command)
		}
	case CommandSet:
		if len(arguments) == 0 {
			return Options{}, fmt.Errorf("set requires a status")
		}
		if len(arguments) != 1 {
			return Options{}, fmt.Errorf("set takes exactly one status")
		}
		options.Status = arguments[0]
	case CommandComment:
		options.Comment = strings.Join(arguments, " ")
	default:
		return Options{}, fmt.Errorf("unknown command %q", positional[0])
	}
	options.Help = helpRequested
	return options, nil
}
