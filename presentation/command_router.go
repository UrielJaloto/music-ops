package presentation

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const (
	CommandRun      = "run"
	CommandPreview  = "preview"
	CommandConfig   = "config"
	CommandDoctor   = "doctor"
	CommandWatch    = "watch"
	CommandAdvisor  = "advisor"
	CommandVersion  = "version"
	commandHelp     = "help"
	exitSuccess     = 0
	exitFailure     = 1
	keyValueDivider = "="
	outputDirKey    = "output.dir"
	outputLayoutKey = "output.layout"
)

var ErrCommandLineInvalid = errors.New("command line is invalid")

var helpArguments = map[string]bool{commandHelp: true, "-h": true, "--help": true}

var commandsWithRawArguments = map[string]bool{CommandConfig: true}

const usageText = `usage: music-ops <command> [arguments] [flags]

commands:
  run [folder]                 import a folder (asks for one when omitted)
  preview [folder]             show the planned result without writing anything
  config show                  print every setting
  config get <key>             print one setting
  config set <key> <value>     change one setting
  config path                  print the settings file location
  doctor                       check ffmpeg, encoder, Last.fm, advisors and output
  watch [inbox]                import every folder that arrives in the inbox
  advisor install              download the local advisor runtime and model
  version                      print the version

flags (every command except config):
  --out <dir>                  output folder (output.dir)
  --layout flat|album_folders  output layout (output.layout)
  --set key=value              override any setting, repeatable
  --verbose                    print detailed logs to the console
  --no-open                    do not open the report and the output folder
`

type CommandController interface {
	Execute(ctx context.Context, arguments []string) int
}

type RouterRequest struct {
	Command    string
	Arguments  []string
	Overrides  map[string]string
	IsVerbose  bool
	OpensFiles bool
}

func ParseCommandLine(arguments []string) (RouterRequest, error) {
	request := RouterRequest{Command: CommandRun, Arguments: []string{}, Overrides: map[string]string{}, OpensFiles: true}
	if len(arguments) > 0 && startsWithCommand(arguments[0]) {
		request.Command = arguments[0]
		arguments = arguments[1:]
	}
	if commandsWithRawArguments[request.Command] || helpArguments[request.Command] {
		request.Arguments = append(request.Arguments, arguments...)
		return request, nil
	}
	return parseCommandFlags(request, arguments)
}

func startsWithCommand(argument string) bool {
	return helpArguments[argument] || !strings.HasPrefix(argument, "-")
}

func parseCommandFlags(request RouterRequest, arguments []string) (RouterRequest, error) {
	flags := flag.NewFlagSet(request.Command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	output := flags.String("out", "", "")
	layout := flags.String("layout", "", "")
	settings := settingFlags{}
	flags.Var(&settings, "set", "")
	verbose := flags.Bool("verbose", false, "")
	noOpen := flags.Bool("no-open", false, "")
	if err := flags.Parse(reorderFlags(arguments)); err != nil {
		return RouterRequest{}, fmt.Errorf("%w: %w", ErrCommandLineInvalid, err)
	}
	for key, value := range settings {
		request.Overrides[key] = value
	}
	addOverride(request.Overrides, outputDirKey, *output)
	addOverride(request.Overrides, outputLayoutKey, *layout)
	request.Arguments = flags.Args()
	request.IsVerbose = *verbose
	request.OpensFiles = !*noOpen
	return request, nil
}

func reorderFlags(arguments []string) []string {
	flags := []string{}
	positional := []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if !strings.HasPrefix(argument, "-") {
			positional = append(positional, argument)
			continue
		}
		flags = append(flags, argument)
		if takesValue(argument) && index+1 < len(arguments) {
			index++
			flags = append(flags, arguments[index])
		}
	}
	return append(flags, positional...)
}

func takesValue(argument string) bool {
	name := strings.TrimLeft(argument, "-")
	return !strings.Contains(name, keyValueDivider) && (name == "out" || name == "layout" || name == "set")
}

func addOverride(overrides map[string]string, key, value string) {
	if value != "" {
		overrides[key] = value
	}
}

type settingFlags map[string]string

func (settings settingFlags) String() string {
	return fmt.Sprint(map[string]string(settings))
}

func (settings settingFlags) Set(value string) error {
	key, setting, hasDivider := strings.Cut(value, keyValueDivider)
	if !hasDivider || strings.TrimSpace(key) == "" {
		return fmt.Errorf("--set expects key=value, got %q", value)
	}
	settings[strings.TrimSpace(key)] = setting
	return nil
}

type CommandRouter struct {
	controllers map[string]CommandController
	output      io.Writer
}

func NewCommandRouter(controllers map[string]CommandController, output io.Writer) CommandRouter {
	return CommandRouter{controllers: controllers, output: output}
}

func (router CommandRouter) Route(ctx context.Context, request RouterRequest) int {
	if helpArguments[request.Command] {
		return router.printUsage(exitSuccess)
	}
	controller, isKnown := router.controllers[request.Command]
	if !isKnown {
		return router.printUsage(exitFailure)
	}
	return controller.Execute(ctx, request.Arguments)
}

func (router CommandRouter) printUsage(code int) int {
	if _, err := io.WriteString(router.output, usageText); err != nil {
		return exitFailure
	}
	return code
}
