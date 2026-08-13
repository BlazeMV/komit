// Command komit is a TUI for selecting changed files and committing them with a
// Claude-generated message.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/ai"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
	"github.com/BlazeMV/komit/internal/ui"
)

// version is set by the linker at release time.
var version = "dev"

// currentVersion prefers the linker's value, then the version Go stamps into
// the binary from VCS tags.
func currentVersion() string {
	var stamped string
	if bi, ok := debug.ReadBuildInfo(); ok {
		stamped = bi.Main.Version
	}
	return resolveVersion(version, stamped)
}

func resolveVersion(linker, stamped string) string {
	if linker != "dev" {
		return linker
	}
	if stamped == "" || stamped == "(devel)" {
		return linker
	}
	return stamped
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintf(stdout, "komit %s\n", currentVersion())
			return 0
		case "init":
			if err := initConfig(stdout); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		default:
			fmt.Fprintf(stderr, "unknown argument %q — usage: komit [init|--version]\n", args[0])
			return 2
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repo, err := git.Open(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "not a git repository (or git is not installed)")
		return 1
	}
	cfg, warnings, err := config.Load(repo.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(stderr, e)
		}
		return 1
	}
	runner, err := ai.New(cfg)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if repo.Loose(config.RepoFile) {
		warnings = append(warnings, config.RepoFile+
			" is untracked and not ignored — it will show up in your change list; add it to .gitignore if it is personal")
	}

	p := tea.NewProgram(ui.New(repo, cfg, runner, warnings))
	_, runErr := p.Run()
	// Command goroutines are abandoned on quit, so their cleanup lands here.
	if err := repo.DrainIntents(); err != nil {
		fmt.Fprintln(stderr, err)
	}
	if runErr != nil {
		fmt.Fprintln(stderr, runErr)
		return 1
	}
	return 0
}

// initConfig writes the built-in defaults to the user config path, refusing to
// overwrite an existing file.
func initConfig(stdout io.Writer) error {
	path, err := config.UserPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists", path)
		}
		return err
	}
	_, err = f.Write(config.DefaultYAML())
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		if err != nil {
			return err
		}
		return closeErr
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return nil
}
