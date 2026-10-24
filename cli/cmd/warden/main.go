// Command warden is the Warden platform CLI (hub DESIGN §11, ADR-017).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/WARDEN-CCD/warden-platform/cli/internal/render"
	"github.com/WARDEN-CCD/warden-platform/cli/internal/verify"
	"github.com/WARDEN-CCD/warden-platform/templates"
)

// version is set at release time with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

const usage = `warden — Warden platform CLI

Usage:
  warden new <name> [--owner TEAM] [--platform-ref vX.Y.Z] [--dry-run]   scaffold and deploy a service
  warden render <name> --out DIR [--digest sha256:...]                    render the template only
  warden verify <image@sha256:...> [--policy-ref REF | --policy-file F]   run admission's checks locally
  warden status <name>                                                    rollout state of a service
  warden dora [--since YYYY-MM-DD] [--json]                               DORA metrics from GitHub
  warden setup-apps                                                       create the promoter/deployments GitHub Apps
  warden version
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "render":
		err = cmdRender(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "dora":
		err = cmdDora(os.Args[2:])
	case "setup-apps":
		err = cmdSetupApps(os.Args[2:])
	case "version", "--version":
		fmt.Println("warden", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		var exit exitError
		if errors.As(err, &exit) {
			os.Exit(int(exit))
		}
		fmt.Fprintln(os.Stderr, "warden:", err)
		os.Exit(1)
	}
}

// exitError ends the process with a status code after output has already been printed.
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

// runWithStdin runs a command feeding secret material on stdin, never as an argument (argv is visible to
// other local processes).
func runWithStdin(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// parse splits leading positional arguments from flags, so `warden render books --out x` works.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		positional = append(positional, args[0])
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return append(positional, fs.Args()...), nil
}

func cmdRender(args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	out := fs.String("out", "", "output directory (required)")
	owner := fs.String("owner", "", "owning GitHub team")
	ref := fs.String("platform-ref", "v0.1.0", "warden-platform workflow tag")
	digest := fs.String("digest", "", "image digest for the gitops tree (sha256:...)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *out == "" {
		return fmt.Errorf("usage: warden render <name> --out DIR [--digest sha256:...]")
	}
	v, err := render.NewValues(pos[0], *owner, *ref)
	if err != nil {
		return err
	}
	files, err := render.Tree(templates.FS, "spring-service/repo", filepath.Join(*out, v.Repo), v)
	if err != nil {
		return err
	}
	fmt.Printf("rendered %d files into %s\n", len(files), filepath.Join(*out, v.Repo))
	if *digest != "" {
		v.Digest = *digest
		files, err = render.Tree(templates.FS, "spring-service/gitops", filepath.Join(*out, "apps", v.Name), v)
		if err != nil {
			return err
		}
		fmt.Printf("rendered %d files into %s\n", len(files), filepath.Join(*out, "apps", v.Name))
	}
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	policyRef := fs.String("policy-ref", "main", "warden-policy git ref to read the admission policy from")
	policyFile := fs.String("policy-file", "", "read the admission policy from a local file instead")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: warden verify <image@sha256:...>")
	}
	p, err := verify.LoadPolicy(*policyFile, *policyRef)
	if err != nil {
		return err
	}
	results, admitted := verify.Image(pos[0], p, verify.Cosign)
	for _, r := range results {
		mark := "✔"
		switch {
		case !r.OK && r.Audit:
			mark = "!"
		case !r.OK:
			mark = "✘"
		}
		fmt.Printf("%s %-17s %s\n", mark, r.Name, r.Detail)
	}
	if admitted {
		fmt.Println("→ would be ADMITTED")
		return nil
	}
	fmt.Println("→ would be DENIED")
	return exitError(1)
}
