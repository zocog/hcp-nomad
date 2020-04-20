// +build ent

package nomad

import (
	"time"

	metrics "github.com/armon/go-metrics"
	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

// License endpoint is used for manipulating an enterprise license
type License struct {
	srv *Server
}

// UpsertLicense is used to set an enterprise license
func (l *License) UpsertLicense(args *structs.LicenseUpsertRequest, reply *structs.GenericResponse) error {
	if done, err := l.srv.forward("License.UpsertLicense", args, args, reply); done {
		return err
	}

	// Check OperatorWrite permissions
	if aclObj, err := l.srv.ResolveToken(args.AuthToken); err != nil {
		return err
	} else if aclObj != nil && !aclObj.AllowOperatorWrite() {
		return structs.ErrPermissionDenied
	}

	defer metrics.MeasureSince([]string{"nomad", "license", "upsert_license"}, time.Now())

	// Update via Raft
	out, index, err := l.srv.raftApply(structs.LicenseUpsertRequestType, args)
	if err != nil {
		return err
	}

	// Check if there was an error when applying
	if err, ok := out.(error); ok && err != nil {
		return err
	}

	// Update the index
	reply.Index = index

	return nil
}

// GetLicense is used to retrieve an enterprise license
func (l *License) GetLicense(args *structs.LicenseGetRequest, reply *structs.LicenseGetResponse) error {
	if done, err := l.srv.forward("License.GetLicense", args, args, reply); done {
		return err
	}

	// Check OperatorRead permissions
	if aclObj, err := l.srv.ResolveToken(args.AuthToken); err != nil {
		return err
	} else if aclObj != nil && !aclObj.AllowOperatorRead() {
		return structs.ErrPermissionDenied
	}

	defer metrics.MeasureSince([]string{"nomad", "license", "get_license"}, time.Now())

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, s *state.StateStore) error {
			out, err := s.License(ws)
			if err != nil {
				return err
			}

			// Set the output
			reply.License = out
			if out != nil {
				reply.Index = out.ModifyIndex
			} else {
				// Use the last index the affected the license table
				index, err := s.Index(state.TableLicense)
				if err != nil {
					return err
				}

				// Ensure we never set the index to zero, otherwise a blocking query cannot be used.
				// We floor the index at one, since realistically the first write must have a higher index.
				if index == 0 {
					index = 1
				}
				reply.Index = index
			}
			return nil
		},
	}
	return l.srv.blockingRPC(&opts)
}
