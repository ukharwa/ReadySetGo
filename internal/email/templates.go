package email

import (
	"bytes"
	"fmt"
	"io/fs"
	"strings"
	"text/template"
)

// Templates renders emails from text/template files. Each file starts with
// a "Subject: …" line, then a blank line, then the body:
//
//	Subject: Confirm your email address
//
//	Open this link to confirm it: {{.Link}}
type Templates struct {
	t *template.Template
}

// ParseTemplates parses the files in fsys matching pattern. Parsing at
// startup means a broken template stops the app starting, not a send.
func ParseTemplates(fsys fs.FS, pattern string) (*Templates, error) {
	t, err := template.New("").Option("missingkey=error").ParseFS(fsys, pattern)
	if err != nil {
		return nil, fmt.Errorf("parse email templates: %w", err)
	}
	return &Templates{t: t}, nil
}

// Message renders the template name for data and addresses it to to.
func (t *Templates) Message(name, to string, data any) (Message, error) {
	var b bytes.Buffer
	if err := t.t.ExecuteTemplate(&b, name, data); err != nil {
		return Message{}, fmt.Errorf("render email %s: %w", name, err)
	}
	head, body, ok := strings.Cut(b.String(), "\n\n")
	subject, hasSubject := strings.CutPrefix(head, "Subject: ")
	if !ok || !hasSubject {
		return Message{}, fmt.Errorf("render email %s: must start with a Subject line and a blank line", name)
	}
	m := Message{To: to, Subject: strings.TrimSpace(subject), Body: body}
	return m, m.Validate()
}
