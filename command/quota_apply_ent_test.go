//go:build ent
// +build ent

package command

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/mitchellh/cli"
	"github.com/shoenig/test/must"
)

func TestQuotaApplyCommand_Good_HCL(t *testing.T) {
	ci.Parallel(t)

	// Create a server
	srv, client, url := testServer(t, true, nil)
	defer srv.Shutdown()

	ui := new(cli.MockUi)
	cmd := &QuotaApplyCommand{Meta: Meta{Ui: ui}}

	fh1, err := ioutil.TempFile("", "nomad")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Remove(fh1.Name())
	if _, err := fh1.WriteString(defaultHclQuotaSpec); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Create a quota spec
	if code := cmd.Run([]string{"-address=" + url, fh1.Name()}); code != 0 {
		t.Fatalf("expected exit 0, got: %d; %v", code, ui.ErrorWriter.String())
	}

	quotas, _, err := client.Quotas().List(nil)
	must.NoError(t, err)
	must.Len(t, 1, quotas)
	must.Len(t, 1, quotas[0].Limits)
	limit := quotas[0].Limits[0]
	must.Eq(t, *limit.RegionLimit.CPU, 2500, must.Sprint("cpu"))
	must.Eq(t, *limit.RegionLimit.MemoryMB, 1000, must.Sprint("memory"))
	must.Eq(t, *limit.RegionLimit.MemoryMaxMB, 1000, must.Sprint("memory_max"))
	must.Eq(t, *limit.VariablesLimit, 1000, must.Sprint("variables_limit"))
}

func TestQuotaApplyCommand_Good_JSON(t *testing.T) {
	ci.Parallel(t)

	// Create a server
	srv, client, url := testServer(t, true, nil)
	defer srv.Shutdown()

	ui := new(cli.MockUi)
	cmd := &QuotaApplyCommand{Meta: Meta{Ui: ui}}

	fh1, err := ioutil.TempFile("", "nomad")
	if err != nil {
		t.Fatalf("err: %s", err)
	}
	defer os.Remove(fh1.Name())
	if _, err := fh1.WriteString(defaultJsonQuotaSpec); err != nil {
		t.Fatalf("err: %s", err)
	}

	// Create a quota spec
	if code := cmd.Run([]string{"-address=" + url, "-json", fh1.Name()}); code != 0 {
		t.Fatalf("expected exit 0, got: %d; %v", code, ui.ErrorWriter.String())
	}

	quotas, _, err := client.Quotas().List(nil)
	must.Nil(t, err)
	must.Len(t, 1, quotas)
}
