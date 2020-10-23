// +build ent

package nomad

import (
	"fmt"
	"time"

	metrics "github.com/armon/go-metrics"
	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/go-msgpack/codec"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/raft"
)

// Offset the Nomad Pro specific values so that we don't overlap
// the OSS/Enterprise values.
const (
	SentinelPolicySnapshot SnapshotType = 65
	QuotaSpecSnapshot      SnapshotType = 66
	QuotaUsageSnapshot     SnapshotType = 67
	LicenseSnapshot        SnapshotType = 68
	TmpLicenseMetaSnapshot SnapshotType = 69
	RecommendationSnapshot SnapshotType = 70
)

// registerEntLogAppliers registers all the Nomad Enterprise Raft log appliers
func (n *nomadFSM) registerEntLogAppliers() {
	n.enterpriseAppliers[structs.SentinelPolicyUpsertRequestType] = n.applySentinelPolicyUpsert
	n.enterpriseAppliers[structs.SentinelPolicyDeleteRequestType] = n.applySentinelPolicyDelete
	n.enterpriseAppliers[structs.QuotaSpecUpsertRequestType] = n.applyQuotaSpecUpsert
	n.enterpriseAppliers[structs.QuotaSpecDeleteRequestType] = n.applyQuotaSpecDelete
	n.enterpriseAppliers[structs.LicenseUpsertRequestType] = n.applyLicenseUpsert
	n.enterpriseAppliers[structs.TmpLicenseUpsertRequestType] = n.applyTmpLicenseMetaUpsert
	n.enterpriseAppliers[structs.RecommendationUpsertRequestType] = n.applyRecommendationUpsert
	n.enterpriseAppliers[structs.RecommendationDeleteRequestType] = n.applyRecommendationDelete
}

// registerEntSnapshotRestorers registers all the Nomad Enterprise snapshot restorers
func (n *nomadFSM) registerEntSnapshotRestorers() {
	n.enterpriseRestorers[SentinelPolicySnapshot] = restoreSentinelPolicy
	n.enterpriseRestorers[QuotaSpecSnapshot] = restoreQuotaSpec
	n.enterpriseRestorers[QuotaUsageSnapshot] = restoreQuotaUsage
	n.enterpriseRestorers[LicenseSnapshot] = restoreLicense
	n.enterpriseRestorers[TmpLicenseMetaSnapshot] = restoreTmpLicenseMeta
	n.enterpriseRestorers[RecommendationSnapshot] = restoreRecommendation
}

// restoreLicense is used to restore a license snapshot
func restoreLicense(restore *state.StateRestore, dec *codec.Decoder) error {
	license := new(structs.StoredLicense)
	if err := dec.Decode(license); err != nil {
		return err
	}
	return restore.LicenseRestore(license)
}

func restoreTmpLicenseMeta(restore *state.StateRestore, dec *codec.Decoder) error {
	meta := new(structs.TmpLicenseMeta)
	if err := dec.Decode(meta); err != nil {
		return err
	}
	return restore.TmpLicenseMetaRestore(meta)
}

// applySentinelPolicyUpsert is used to upsert a set of policies
func (n *nomadFSM) applySentinelPolicyUpsert(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_sentinel_policy_upsert"}, time.Now())
	var req structs.SentinelPolicyUpsertRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.UpsertSentinelPolicies(index, req.Policies); err != nil {
		n.logger.Error("UpsertSentinelPolicies failed", "error", err)
		return err
	}
	return nil
}

// applySentinelPolicyDelete is used to delete a set of policies
func (n *nomadFSM) applySentinelPolicyDelete(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_sentinel_policy_delete"}, time.Now())
	var req structs.SentinelPolicyDeleteRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.DeleteSentinelPolicies(index, req.Names); err != nil {
		n.logger.Error("DeleteSentinelPolicies failed", "error", err)
		return err
	}
	return nil
}

// applyQuotaSpecUpsert is used to upsert a set of quota specifications
func (n *nomadFSM) applyQuotaSpecUpsert(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_quota_spec_upsert"}, time.Now())
	var req structs.QuotaSpecUpsertRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.UpsertQuotaSpecs(index, req.Quotas); err != nil {
		n.logger.Error("UpsertQuotaSpecs failed", "error", err)
		return err
	}

	// Unblock the quotas. This will be a no-op if there are no evals blocked on
	// the quota.
	for _, q := range req.Quotas {
		n.blockedEvals.UnblockQuota(q.Name, index)
	}

	return nil
}

// applyQuotaSpecDelete is used to delete a set of policies
func (n *nomadFSM) applyQuotaSpecDelete(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_quota_spec_delete"}, time.Now())
	var req structs.QuotaSpecDeleteRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.DeleteQuotaSpecs(index, req.Names); err != nil {
		n.logger.Error("DeleteQuotaSpecs failed", "error", err)
		return err
	}
	return nil
}

// allocQuota returns the quota object associated with the allocation.
func (n *nomadFSM) allocQuota(allocID string) (string, error) {
	alloc, err := n.state.AllocByID(nil, allocID)
	if err != nil {
		return "", err
	}

	// Guard against the client updating a non-existent allocation.
	if alloc == nil {
		return "", nil
	}

	ns, err := n.state.NamespaceByName(nil, alloc.Namespace)
	if err != nil {
		return "", err
	}
	if ns == nil {
		return "", fmt.Errorf("unknown namespace %q attached to alloc %q", alloc.Namespace, alloc.ID)
	}

	return ns.Quota, nil
}

// applyLicenseUpsert is used to upsert a new license
func (n *nomadFSM) applyLicenseUpsert(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_license_upsert"}, time.Now())
	var req structs.LicenseUpsertRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.UpsertLicense(index, req.License); err != nil {
		n.logger.Error("UpsertLicense failed", "error", err)
		return err
	}
	return nil
}

func (n *nomadFSM) applyTmpLicenseMetaUpsert(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_tmp_license_meta_upsert"}, time.Now())
	var req structs.TmpLicenseMeta
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.TmpLicenseSetMeta(index, &req); err != nil {
		n.logger.Error("UpsertLicense failed", "error", err)
		return err
	}
	return nil
}

// restoreSentinelPolicy is used to restore a sentinel policy
func restoreSentinelPolicy(restore *state.StateRestore, dec *codec.Decoder) error {
	policy := new(structs.SentinelPolicy)
	if err := dec.Decode(policy); err != nil {
		return err
	}
	return restore.SentinelPolicyRestore(policy)
}

// restoreQuotaSpec is used to restore a quota spec
func restoreQuotaSpec(restore *state.StateRestore, dec *codec.Decoder) error {
	spec := new(structs.QuotaSpec)
	if err := dec.Decode(spec); err != nil {
		return err
	}
	return restore.QuotaSpecRestore(spec)
}

// restoreQuotaUsage is used to restore a quota usage
func restoreQuotaUsage(restore *state.StateRestore, dec *codec.Decoder) error {
	usage := new(structs.QuotaUsage)
	if err := dec.Decode(usage); err != nil {
		return err
	}
	return restore.QuotaUsageRestore(usage)
}

// persistEntTables persists all the Nomad Enterprise state store tables.
func (s *nomadSnapshot) persistEntTables(sink raft.SnapshotSink, encoder *codec.Encoder) error {
	if err := s.persistSentinelPolicies(sink, encoder); err != nil {
		sink.Cancel()
		return err
	}
	if err := s.persistQuotaSpecs(sink, encoder); err != nil {
		sink.Cancel()
		return err
	}
	if err := s.persistQuotaUsages(sink, encoder); err != nil {
		sink.Cancel()
		return err
	}
	if err := s.persistLicense(sink, encoder); err != nil {
		return err
	}
	if err := s.persistTmpLicenseMeta(sink, encoder); err != nil {
		return err
	}
	if err := s.persistRecommendations(sink, encoder); err != nil {
		return err
	}
	return nil
}

// persistSentinelPolicies is used to persist sentinel policies
func (s *nomadSnapshot) persistSentinelPolicies(sink raft.SnapshotSink,
	encoder *codec.Encoder) error {
	// Get all the policies
	ws := memdb.NewWatchSet()
	policies, err := s.snap.SentinelPolicies(ws)
	if err != nil {
		return err
	}

	for {
		// Get the next item
		raw := policies.Next()
		if raw == nil {
			break
		}

		// Prepare the request struct
		policy := raw.(*structs.SentinelPolicy)

		// Write out a policy registration
		sink.Write([]byte{byte(SentinelPolicySnapshot)})
		if err := encoder.Encode(policy); err != nil {
			return err
		}
	}
	return nil
}

// persistQuotaSpecs is used to persist quota specifications
func (s *nomadSnapshot) persistQuotaSpecs(sink raft.SnapshotSink,
	encoder *codec.Encoder) error {
	// Get all the specs
	ws := memdb.NewWatchSet()
	policies, err := s.snap.QuotaSpecs(ws)
	if err != nil {
		return err
	}

	for {
		// Get the next item
		raw := policies.Next()
		if raw == nil {
			break
		}

		// Prepare the request struct
		spec := raw.(*structs.QuotaSpec)

		// Write out a spec registration
		sink.Write([]byte{byte(QuotaSpecSnapshot)})
		if err := encoder.Encode(spec); err != nil {
			return err
		}
	}
	return nil
}

// persistQuotaUsages is used to persist quota usages
func (s *nomadSnapshot) persistQuotaUsages(sink raft.SnapshotSink,
	encoder *codec.Encoder) error {
	// Get all the usages
	ws := memdb.NewWatchSet()
	policies, err := s.snap.QuotaUsages(ws)
	if err != nil {
		return err
	}

	for {
		// Get the next item
		raw := policies.Next()
		if raw == nil {
			break
		}

		// Prepare the request struct
		usage := raw.(*structs.QuotaUsage)

		// Write out a spec registration
		sink.Write([]byte{byte(QuotaUsageSnapshot)})
		if err := encoder.Encode(usage); err != nil {
			return err
		}
	}
	return nil
}

// persistLicense is used to persist license
func (s *nomadSnapshot) persistLicense(sink raft.SnapshotSink, enc *codec.Encoder) error {
	ws := memdb.NewWatchSet()
	license, err := s.snap.License(ws)
	if err != nil {
		return err
	}

	sink.Write([]byte{byte(LicenseSnapshot)})
	if err := enc.Encode(license); err != nil {
		return err
	}

	return nil
}

func (s *nomadSnapshot) persistTmpLicenseMeta(sink raft.SnapshotSink, enc *codec.Encoder) error {
	ws := memdb.NewWatchSet()
	meta, err := s.snap.TmpLicenseMeta(ws)
	if err != nil {
		return err
	}
	if meta == nil {
		return nil
	}

	sink.Write([]byte{byte(TmpLicenseMetaSnapshot)})
	if err := enc.Encode(meta); err != nil {
		return err
	}

	return nil
}

// persistRecommendations is used to persist sizing recommendations.
func (s *nomadSnapshot) persistRecommendations(sink raft.SnapshotSink, enc *codec.Encoder) error {
	ws := memdb.NewWatchSet()
	recs, err := s.snap.Recommendations(ws)
	if err != nil {
		return err
	}

	for {
		// Get the next item
		raw := recs.Next()
		if raw == nil {
			break
		}

		// Prepare the request struct
		rec := raw.(*structs.Recommendation)

		sink.Write([]byte{byte(RecommendationSnapshot)})
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil

}

func (n *nomadFSM) applyRecommendationUpsert(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_recommendation_upsert"}, time.Now())
	var req structs.RecommendationUpsertRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.UpsertRecommendation(index, req.Recommendation); err != nil {
		n.logger.Error("UpsertRecommendation failed", "error", err)
		return err
	}

	return nil
}

func (n *nomadFSM) applyRecommendationDelete(buf []byte, index uint64) interface{} {
	defer metrics.MeasureSince([]string{"nomad", "fsm", "apply_recommendation_delete"}, time.Now())
	var req structs.RecommendationDeleteRequest
	if err := structs.Decode(buf, &req); err != nil {
		panic(fmt.Errorf("failed to decode request: %v", err))
	}

	if err := n.state.DeleteRecommendations(index, req.Recommendations); err != nil {
		n.logger.Error("DeleteRecommendations failed", "error", err)
		return err
	}

	return nil
}

// restoreRecommendation is used to restore a recommendation snapshot
func restoreRecommendation(restore *state.StateRestore, dec *codec.Decoder) error {
	rec := new(structs.Recommendation)
	if err := dec.Decode(rec); err != nil {
		return err
	}
	return restore.RecommendationRestore(rec)
}
