package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestRequestIDAttribute(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := New(&buf, "json", slog.LevelInfo).With("component", "test")

	logger.InfoContext(WithRequestID(context.Background(), "req-1"), "with id")
	logger.InfoContext(context.Background(), "without id")

	dec := json.NewDecoder(&buf)
	for _, want := range []string{"req-1", ""} {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		got, _ := rec["request_id"].(string)
		if got != want {
			t.Errorf("%s: request_id = %q, want %q", rec["msg"], got, want)
		}
		if rec["component"] != "test" {
			t.Errorf("%s: lost attribute added with With", rec["msg"])
		}
	}
}
