// +build ent

package nomad

import (
	"errors"
	"time"

	metrics "github.com/armon/go-metrics"
	"github.com/hashicorp/nomad/nomad/structs"
)

// License endpoint is used for manipulating an enterprise license
type License struct {
	srv *Server
}

// COMPAT: License.UpsertLicense was deprecated in Nomad 1.1.0
// UpsertLicense is used to set an enterprise license
func (l *License) UpsertLicense(args *structs.LicenseUpsertRequest, reply *structs.GenericResponse) error {
	return errors.New("License.UpsertLicense is deprecated")
}

// GetLicense is used to retrieve an enterprise license
func (l *License) GetLicense(args *structs.LicenseGetRequest, reply *structs.LicenseGetResponse) error {
	if done, err := l.srv.forward("License.GetLicense", args, args, reply); done {
		return err
	}

	defer metrics.MeasureSince([]string{"nomad", "license", "get_license"}, time.Now())

	// Check OperatorRead permissions
	if aclObj, err := l.srv.ResolveToken(args.AuthToken); err != nil {
		return err
	} else if aclObj != nil && !aclObj.AllowOperatorRead() {
		return structs.ErrPermissionDenied
	}

	out := l.srv.EnterpriseState.License()
	reply.NomadLicense = out
	reply.ConfigOutdated = l.srv.EnterpriseState.FileLicenseOutdated()

	return nil
}
