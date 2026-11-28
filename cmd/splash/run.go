package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"gosplash.dev/splash/internal/vm"
)

func runVMCommand(args []string, output io.Writer) error {
	const usage = "usage: splash run <file.splash> --backend vm [--max-instructions N] [--max-frames N]"
	options := flag.NewFlagSet("splash run", flag.ContinueOnError)
	options.SetOutput(io.Discard)
	backend := options.String("backend", "", "execution backend (vm required)")
	instructions := options.Uint64("max-instructions", 1_000_000, "maximum dispatched VM instructions")
	frames := options.Int("max-frames", 256, "maximum active VM frames, including main")
	help := func() {
		fmt.Fprintln(output, usage)
		options.SetOutput(output)
		options.PrintDefaults()
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-help" || args[0] == "-h") {
		help()
		return nil
	}
	if len(args) == 0 || args[0] == "" || strings.HasPrefix(args[0], "-") {
		return errors.New(usage)
	}
	if err := options.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			help()
			return nil
		}
		return fmt.Errorf("run: %w", err)
	}
	if options.NArg() != 0 {
		return fmt.Errorf("run: unexpected argument %q", options.Arg(0))
	}
	if *backend == "" {
		return errors.New(usage)
	}
	if *backend != "vm" {
		return fmt.Errorf("run: unsupported backend %q, use --backend vm", *backend)
	}
	if *frames < 0 {
		return errors.New("run: --max-frames must be nonnegative")
	}
	program, err := analyzeProgram(args[0])
	if err != nil {
		return fmt.Errorf("source analysis: %w", err)
	}
	verified, err := verifyProgram(program)
	if err != nil {
		return err
	}
	return verified.RunVM(output, vm.Limits{Instructions: *instructions, Frames: *frames})
}
