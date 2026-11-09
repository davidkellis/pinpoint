// refcheck replays versioned development cases without modifying expectations.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"pinpoint/internal/reference"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("use two-hyphen options, for example --suite audio")
		}
	}
	fs := flag.NewFlagSet("refcheck", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: refcheck [--suite quick|audio|location|all] [--out DIRECTORY]")
		fmt.Fprintln(fs.Output(), "  --suite     Checks to run (default: quick)")
		fmt.Fprintln(fs.Output(), "  --root      Project root (default: .)")
		fmt.Fprintln(fs.Output(), "  --manifest  Manifest relative to root (default: references/suite.json)")
		fmt.Fprintln(fs.Output(), "  --out       Report directory relative to root (default: work/reference-check)")
	}
	profile := fs.String("suite", "quick", "quick, audio, location, or all")
	root := fs.String("root", ".", "project root")
	manifest := fs.String("manifest", "references/suite.json", "manifest relative to project root")
	out := fs.String("out", "work/reference-check", "report directory relative to project root")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r, runErr := reference.Run(ctx, abs, *manifest, *profile, func(s string) { fmt.Println(s) })
	if runErr != nil && r.Status != "fail" {
		r.Status = "error"
		r.Errors = append(r.Errors, runErr.Error())
	}
	// Record the decoder version as well as Go and source/input digests.
	ffmpeg, _ := exec.CommandContext(ctx, "ffmpeg", "-version").Output()
	version := strings.SplitN(string(ffmpeg), "\n", 2)[0]
	payload := struct {
		reference.Report
		Decoder string `json:"decoder"`
	}{r, version}
	dir := filepath.Join(abs, *out)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, *profile+".json")
	if err = os.WriteFile(p, append(b, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println("Report:", p)
	return runErr
}
