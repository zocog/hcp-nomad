//go:build ent

package nomad

import (
	"testing"
	"time"

	"github.com/hashicorp/go-licensing/v3"
	"github.com/hashicorp/nomad/ci"
	"github.com/shoenig/test/must"
)

func TestLicenseConfig_Validate(t *testing.T) {
	ci.Parallel(t)

	t.Run("ok", func(t *testing.T) {
		lc := defaultTestLicenseConfig()
		err := lc.Validate()
		must.NoError(t, err)
	})

	// other error modes are covered in licensing lib and our watcher tests
	t.Run("expired", func(t *testing.T) {
		lc := defaultTestLicenseConfig()
		lc.BuildDate = time.Now().Add(time.Hour * 24 * 365)
		err := lc.Validate()
		must.Error(t, err)
		must.ErrorIs(t, err, licensing.ErrExpirationBeforeBuildDate)
	})
}
