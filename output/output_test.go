package output_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/JaimeStill/spike-messaging/output"
)

func TestStreams(t *testing.T) {
	var out, errs bytes.Buffer
	o := output.New(&out, &errs)
	o.Printf("handled %d", 3)
	o.Error(errors.New("boom"))
	if out.String() != "handled 3\n" {
		t.Errorf("stdout %q", out.String())
	}
	if errs.String() != "error: boom\n" {
		t.Errorf("stderr %q", errs.String())
	}
}
