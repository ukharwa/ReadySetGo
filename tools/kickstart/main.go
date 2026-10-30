// Command kickstart turns this template into a new project, and checks that
// every combination of options still produces a working one. It exists only
// in the template: init deletes it.
//
//	go run ./tools/kickstart init -name myapp [-module path] [-db sqlite|postgres]
//	go run ./tools/kickstart check [-db sqlite|postgres] [-keep]
//
// The Makefile's `init` and `template-check` targets wrap these.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

const templateModule = "github.com/ukharwa/kickstart"

var (
	databases  = []string{"sqlite", "postgres"}
	uiModes    = []string{"html", "api"}
	allModules = []string{"workspaces", "uploads", "admin", "audit", "metrics", "errreport"}
	validName  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// options are the choices a new project is created with.
type options struct {
	name    string
	module  string
	db      string
	ui      string
	modules []string

	dialectDirs []string // the chosen database's folders; set by initProject
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: kickstart init|check [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = runInit(os.Args[2:])
	case "check":
		err = runCheck(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q (want init or check)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// parseOptions reads the shared flags. Empty values mean "use the default",
// because make passes unset variables as empty strings.
func parseOptions(fs *flag.FlagSet, args []string) (options, error) {
	var o options
	var modules string
	fs.StringVar(&o.name, "name", "", "project name: lowercase letters, digits and hyphens")
	fs.StringVar(&o.module, "module", "", "Go module path (default github.com/ukharwa/NAME)")
	fs.StringVar(&o.db, "db", "", "database: sqlite or postgres (default sqlite)")
	fs.StringVar(&o.ui, "ui", "", "UI mode: html or api (default html)")
	fs.StringVar(&modules, "modules", "", "comma-separated optional modules: "+strings.Join(allModules, ", "))
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if o.db == "" {
		o.db = "sqlite"
	}
	if o.ui == "" {
		o.ui = "html"
	}
	for m := range strings.SplitSeq(modules, ",") {
		if m = strings.TrimSpace(m); m != "" && !slices.Contains(o.modules, m) {
			o.modules = append(o.modules, m)
		}
	}
	return o, nil
}

func (o *options) validate() error {
	var errs []error
	if !validName.MatchString(o.name) {
		errs = append(errs, fmt.Errorf("NAME %q: use lowercase letters, digits and hyphens, starting with a letter", o.name))
	}
	if o.module == "" {
		o.module = "github.com/ukharwa/" + o.name
	}
	if !slices.Contains(databases, o.db) {
		errs = append(errs, fmt.Errorf("DB %q: want one of %s", o.db, strings.Join(databases, ", ")))
	}
	if !slices.Contains(uiModes, o.ui) {
		errs = append(errs, fmt.Errorf("UI %q: want one of %s", o.ui, strings.Join(uiModes, ", ")))
	}
	for _, m := range o.modules {
		if !slices.Contains(allModules, m) {
			errs = append(errs, fmt.Errorf("module %q: want any of %s", m, strings.Join(allModules, ", ")))
		}
	}
	return errors.Join(errs...)
}
