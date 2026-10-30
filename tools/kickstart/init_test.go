package main

import (
	"strings"
	"testing"
)

var sqliteHTML = options{name: "myapp", module: "github.com/me/myapp", db: "sqlite", ui: "html"}
var postgresAPI = options{name: "myapp", module: "github.com/me/myapp", db: "postgres", ui: "api", modules: []string{"uploads"}}

func TestApplyMarkers(t *testing.T) {
	t.Parallel()
	in := `a
// kickstart:begin db=sqlite
sqlite
// kickstart:end
# kickstart:begin db=postgres
postgres
  # kickstart:begin module=uploads
nested uploads
  # kickstart:end
# kickstart:end
<!-- kickstart:begin template -->
template only
<!-- kickstart:end -->
-- kickstart:begin ui=api
api
-- kickstart:end
prose mentioning kickstart:begin is not a marker
b
`
	tests := []struct {
		o    options
		want string
	}{
		{sqliteHTML, "a\nsqlite\nprose mentioning kickstart:begin is not a marker\nb\n"},
		{postgresAPI, "a\npostgres\nnested uploads\napi\nprose mentioning kickstart:begin is not a marker\nb\n"},
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
		"unclosed":          "// kickstart:begin db=sqlite\nx\n",
		"unopened":          "x\n// kickstart:end\n",
		"unknown condition": "// kickstart:begin db=oracle\n// kickstart:end\n",
		"end with argument": "// kickstart:begin db=sqlite\n// kickstart:end db=sqlite\n",
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
		{"tools/kickstart/main.go", sqliteHTML, ""},
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
		{"cmd/kickstart/main.go", sqliteHTML, "cmd/myapp/main.go"},
		{"internal/db/db.go", postgresAPI, "internal/db/db.go"},
		{"internal/auth/web.go", postgresAPI, ""},
		{"internal/auth/templates/login.html", postgresAPI, ""},
		{"internal/web/static/app.css", postgresAPI, ""},
		{"internal/auth/api.go", postgresAPI, "internal/auth/api.go"},
		{"internal/auth/web_test.go", sqliteHTML, "internal/auth/web_test.go"},
		{"internal/auth/api_test.go", sqliteHTML, ""},
		{"internal/auth/emails/verify_email.txt", sqliteHTML, "internal/auth/emails/verify_email.txt"},
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
	in := `import "github.com/ukharwa/kickstart/migrations/postgres"
- internal/jobs/postgres/claim.sql
image: postgres:18-alpine
out: internal/store/postgres
BIN := bin/kickstart
name: kickstart-dev
kickstarter stays`
	got := rewrite("Makefile", in, o)
	for _, want := range []string{
		`import "github.com/me/myapp/migrations"`,
		"- internal/jobs/claim.sql",
		"image: postgres:18-alpine",
		"out: internal/store\n",
		"BIN := bin/myapp",
		"name: myapp-dev",
		"kickstarter stays",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite result lacks %q:\n%s", want, got)
		}
	}
	if got := rewrite("DECISIONS.md", "the kickstart template", postgresAPI); got != "the kickstart template" {
		t.Errorf("DECISIONS.md was renamed: %q", got)
	}
}
