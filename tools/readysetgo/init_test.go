package main

import (
	"strings"
	"testing"
)

var sqliteHTML = options{name: "myapp", module: "github.com/me/myapp", db: "sqlite", ui: "html"}
var postgresAPI = options{name: "myapp", module: "github.com/me/myapp", db: "postgres", ui: "api", modules: []string{"uploads"}}
var withWorkspaces = options{name: "myapp", module: "github.com/me/myapp", db: "sqlite", ui: "html", modules: []string{"workspaces"}}

func TestApplyMarkers(t *testing.T) {
	t.Parallel()
	in := `a
// readysetgo:begin db=sqlite
sqlite
// readysetgo:end
# readysetgo:begin db=postgres
postgres
  # readysetgo:begin module=uploads
nested uploads
  # readysetgo:end
# readysetgo:end
<!-- readysetgo:begin template -->
template only
<!-- readysetgo:end -->
-- readysetgo:begin ui=api
api
-- readysetgo:end
# readysetgo:begin db=sqlite,module=uploads
sqlite or uploads
# readysetgo:end
prose mentioning readysetgo:begin is not a marker
b
`
	tests := []struct {
		o    options
		want string
	}{
		{sqliteHTML, "a\nsqlite\nsqlite or uploads\nprose mentioning readysetgo:begin is not a marker\nb\n"},
		{postgresAPI, "a\npostgres\nnested uploads\napi\nsqlite or uploads\nprose mentioning readysetgo:begin is not a marker\nb\n"},
		{options{db: "postgres", ui: "html"}, "a\npostgres\nprose mentioning readysetgo:begin is not a marker\nb\n"},
	}
	for _, tt := range tests {
		got, err := applyMarkers(in, tt.o)
		if err != nil {
			t.Fatalf("db=%s: %v", tt.o.db, err)
		}
		if got != tt.want {
			t.Errorf("db=%s:\ngot  %q\nwant %q", tt.o.db, got, tt.want)
		}
	}
}

func TestApplyMarkersErrors(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"unclosed":          "// readysetgo:begin db=sqlite\nx\n",
		"unopened":          "x\n// readysetgo:end\n",
		"unknown condition": "// readysetgo:begin db=oracle\n// readysetgo:end\n",
		"unknown in a list": "// readysetgo:begin db=sqlite,db=oracle\n// readysetgo:end\n",
		"end with argument": "// readysetgo:begin db=sqlite\n// readysetgo:end db=sqlite\n",
	} {
		if _, err := applyMarkers(in, sqliteHTML); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestApplyBuildConstraint(t *testing.T) {
	t.Parallel()
	pg := "//go:build postgres\n\npackage db\n"
	notPG := "//go:build !postgres\n\npackage db\n"
	other := "//go:build linux\n\npackage db\n"

	if keep, _ := applyBuildConstraint(pg, sqliteHTML); keep {
		t.Error("postgres file kept in a sqlite project")
	}
	if keep, rest := applyBuildConstraint(pg, postgresAPI); !keep || rest != "package db\n" {
		t.Errorf("postgres file in postgres project: keep=%v rest=%q", keep, rest)
	}
	if keep, rest := applyBuildConstraint(notPG, sqliteHTML); !keep || rest != "package db\n" {
		t.Errorf("sqlite file in sqlite project: keep=%v rest=%q", keep, rest)
	}
	if keep, _ := applyBuildConstraint(notPG, postgresAPI); keep {
		t.Error("sqlite file kept in a postgres project")
	}
	if keep, rest := applyBuildConstraint(other, sqliteHTML); !keep || rest != other {
		t.Errorf("unrelated constraint changed: keep=%v rest=%q", keep, rest)
	}
}

func TestPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rel  string
		o    options
		want string // "" means removed
	}{
		{"tools/readysetgo/main.go", sqliteHTML, ""},
		{"LICENSE", sqliteHTML, ""},
		{"tools/removeexample/main.go", sqliteHTML, "tools/removeexample/main.go"},
		{".github/workflows/template.yml", sqliteHTML, ""},
		{"migrations/sqlite/00001_users.sql", sqliteHTML, "migrations/00001_users.sql"},
		{"migrations/postgres/00001_users.sql", sqliteHTML, ""},
		{"migrations/postgres/00001_users.sql", postgresAPI, "migrations/00001_users.sql"},
		{"internal/auth/store/models.go", sqliteHTML, "internal/auth/store/models.go"},
		{"internal/auth/store/models.go", postgresAPI, ""},
		{"internal/auth/store/postgres/models.go", sqliteHTML, ""},
		{"internal/auth/store/postgres/models.go", postgresAPI, "internal/auth/store/models.go"},
		{"internal/jobs/sqlite/claim.sql", sqliteHTML, "internal/jobs/claim.sql"},
		{"internal/jobs/postgres/claim.sql", sqliteHTML, ""},
		{"internal/jobs/jobs.sql", postgresAPI, "internal/jobs/jobs.sql"},
		{"cmd/readysetgo/main.go", sqliteHTML, "cmd/myapp/main.go"},
		{"internal/db/db.go", postgresAPI, "internal/db/db.go"},
		{"deploy/backup-postgres.sh", sqliteHTML, ""},
		{"deploy/backup-postgres.sh", postgresAPI, "deploy/backup-postgres.sh"},
		{"internal/auth/web.go", postgresAPI, ""},
		{"internal/auth/templates/login.html", postgresAPI, ""},
		{"internal/web/static/app.css", postgresAPI, ""},
		{"internal/auth/api.go", postgresAPI, "internal/auth/api.go"},
		{"internal/auth/web_test.go", sqliteHTML, "internal/auth/web_test.go"},
		{"internal/auth/api_test.go", sqliteHTML, ""},
		{"internal/auth/emails/verify_email.txt", sqliteHTML, "internal/auth/emails/verify_email.txt"},
		{"internal/workspaces/workspaces.go", sqliteHTML, ""},
		{"internal/workspaces/workspaces.go", postgresAPI, ""},
		{"internal/workspaces/workspaces.go", withWorkspaces, "internal/workspaces/workspaces.go"},
	}
	for _, tt := range tests {
		tt.o.replacedDirs = replacedDirs([]string{"internal/auth/store/postgres/db.go", "migrations/postgres/migrations.go", "internal/jobs/postgres/claim.sql"}, tt.o.db)
		got := ""
		if keepPath(tt.rel, tt.o) {
			got = movePath(tt.rel, tt.o)
		}
		if got != tt.want {
			t.Errorf("%s (db=%s) → %q, want %q", tt.rel, tt.o.db, got, tt.want)
		}
	}
}

func TestDialectDirs(t *testing.T) {
	t.Parallel()
	files := []string{"migrations/postgres/1.sql", "internal/store/postgres/db.go", "internal/store/db.go", "internal/jobs/postgres/claim.sql", "internal/jobs/sqlite/claim.sql"}
	got := dialectDirs(files, "postgres")
	want := []string{"internal/store/postgres", "internal/jobs/postgres", "migrations/postgres"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("dialectDirs = %v, want %v", got, want)
	}
}

func TestRewrite(t *testing.T) {
	t.Parallel()
	o := postgresAPI
	o.dialectDirs = []string{"internal/store/postgres", "internal/jobs/postgres", "migrations/postgres"}
	in := `import "github.com/ukharwa/readysetgo/migrations/postgres"
- internal/jobs/postgres/claim.sql
image: postgres:18-alpine
out: internal/store/postgres
BIN := bin/readysetgo
name: readysetgo-dev
# ReadySetGo
readysetgoer stays`
	got := rewrite("Makefile", in, o)
	for _, want := range []string{
		`import "github.com/me/myapp/migrations"`,
		"- internal/jobs/claim.sql",
		"image: postgres:18-alpine",
		"out: internal/store\n",
		"BIN := bin/myapp",
		"name: myapp-dev",
		"# myapp\n",
		"readysetgoer stays",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite result lacks %q:\n%s", want, got)
		}
	}
	if got := rewrite("compose.yaml", "image: ghcr.io/ukharwa/readysetgo:latest", postgresAPI); got != "image: ghcr.io/me/myapp:latest" {
		t.Errorf("image for a GitHub module: %q", got)
	}
	other := postgresAPI
	other.module = "example.com/myapp"
	if got := rewrite("compose.yaml", "image: ghcr.io/ukharwa/readysetgo:latest", other); got != "image: myapp:latest" {
		t.Errorf("image for another module: %q", got)
	}
	if got := rewrite("DECISIONS.md", "the ReadySetGo template", postgresAPI); got != "the ReadySetGo template" {
		t.Errorf("DECISIONS.md was renamed: %q", got)
	}
}

// TestTransformDropsEmptiedFiles checks that a file made only of another
// option's blocks is dropped rather than left empty.
func TestTransformDropsEmptiedFiles(t *testing.T) {
	t.Parallel()
	in := []byte("# readysetgo:begin db=sqlite\ndbs: []\n# readysetgo:end\n")
	if _, _, keep, err := transform("deploy/litestream.yml", in, postgresAPI); err != nil || keep {
		t.Errorf("keep = %v, err = %v; want the emptied file dropped", keep, err)
	}
	if _, out, keep, _ := transform("deploy/litestream.yml", in, sqliteHTML); !keep || string(out) != "dbs: []\n" {
		t.Errorf("sqlite: keep = %v, out = %q", keep, out)
	}
}

func TestDropRedundantAliases(t *testing.T) {
	t.Parallel()
	in := "import (\n\tmigrations \"example.com/app/migrations\"\n\tsqlite3 \"modernc.org/sqlite/lib\"\n)\n"
	want := "import (\n\t\"example.com/app/migrations\"\n\tsqlite3 \"modernc.org/sqlite/lib\"\n)\n"
	if got := dropRedundantAliases(in); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJoinLocalImports(t *testing.T) {
	t.Parallel()
	in := "import (\n\t\"fmt\"\n\n\t\"example.com/app/a\"\n\n\t\"example.com/app/b\"\n\n\t\"example.com/app/c\"\n)\n"
	want := "import (\n\t\"fmt\"\n\n\t\"example.com/app/a\"\n\t\"example.com/app/b\"\n\t\"example.com/app/c\"\n)\n"
	if got := joinLocalImports(in, "example.com/app"); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRecordInit(t *testing.T) {
	t.Parallel()
	o := postgresAPI
	o.commit = "abc123def456"
	got := recordInit("Made by `INIT_COMMAND` from commit INIT_COMMIT.\n", o)
	want := "Made by `make init NAME=myapp MODULE=github.com/me/myapp DB=postgres UI=api MODULES=uploads` from commit abc123def456.\n"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if got := recordInit("INIT_COMMIT", sqliteHTML); got != "unknown" {
		t.Errorf("without a commit: %q", got)
	}
	if got := sqliteHTML.command(); !strings.HasSuffix(got, " MODULES=none") {
		t.Errorf("no modules: %q", got)
	}
}

func TestValidateRequiresModule(t *testing.T) {
	t.Parallel()
	o := options{name: "myapp", db: "sqlite", ui: "html"}
	if err := o.validate(); err == nil || !strings.Contains(err.Error(), "MODULE is required") {
		t.Errorf("err = %v", err)
	}
}

func TestValidateRejectsTemplateName(t *testing.T) {
	t.Parallel()
	o := options{name: "readysetgo", db: "sqlite", ui: "html"}
	if err := o.validate(); err == nil || !strings.Contains(err.Error(), "template's own name") {
		t.Errorf("err = %v", err)
	}
}
