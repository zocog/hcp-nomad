// +build ent

package structs

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/errwrap"
	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad-licensing/license"
	"github.com/hashicorp/sentinel/lang/ast"
	"github.com/hashicorp/sentinel/lang/parser"
	"github.com/hashicorp/sentinel/lang/semantic"
	"github.com/hashicorp/sentinel/lang/token"
	"golang.org/x/crypto/blake2b"
)

// Offset the Nomad Pro specific values so that we don't overlap
// the OSS/Enterprise values.
const (
	SentinelPolicyUpsertRequestType MessageType = 66
	SentinelPolicyDeleteRequestType MessageType = 67
	QuotaSpecUpsertRequestType      MessageType = 68
	QuotaSpecDeleteRequestType      MessageType = 69
	LicenseUpsertRequestType        MessageType = 70
	LicenseDeleteRequestType        MessageType = 71
	TmpLicenseUpsertRequestType     MessageType = 72
	RecommendationUpsertRequestType MessageType = 73
	RecommendationDeleteRequestType MessageType = 74
)

// Restrict the possible Sentinel policy types
const (
	// SentinelEnforcementLevelAdvisory allows a policy to fail and issues a warning
	SentinelEnforcementLevelAdvisory = "advisory"

	// SentinelEnforcementLevelSoftMandatory prevents an operation unless an override is set, and then warns
	SentinelEnforcementLevelSoftMandatory = "soft-mandatory"

	// SentinelEnforcementLevelHardMandatory prevents an operation on failure
	SentinelEnforcementLevelHardMandatory = "hard-mandatory"
)

// Restrict the possible Sentinel scopes
const (
	SentinelScopeSubmitJob = "submit-job"
)

// TmpLicenseMeta tracks the create time for the first temporary license
type TmpLicenseMeta struct {
	CreateTime int64
}

// StoredLicense is used to store and retrieve the signed license blob
// used for checking enterprise features
type StoredLicense struct {
	Signed string

	// Raft Indexes
	CreateIndex uint64
	ModifyIndex uint64
}

// LicenseUpsertRequest is used to store a signed license text blob
type LicenseUpsertRequest struct {
	License *StoredLicense
	WriteRequest
}

// LicenseGetRequest is used to request a signed license text blob
type LicenseGetRequest struct {
	QueryOptions
}

// LicenseGetResponse is used to respond to a request for a parsed License
type LicenseGetResponse struct {
	NomadLicense *license.License
	QueryMeta
}

// SentinelPolicy is used to represent a Sentinel policy
type SentinelPolicy struct {
	Name             string // Unique name
	Description      string // Human readable
	Scope            string // Where should this policy be executed
	EnforcementLevel string // Enforcement Level
	Policy           string
	Hash             []byte
	CreateIndex      uint64
	ModifyIndex      uint64
}

type SentinelPolicyListStub struct {
	Name             string
	Description      string
	Scope            string
	EnforcementLevel string
	Hash             []byte
	CreateIndex      uint64
	ModifyIndex      uint64
}

func (s *SentinelPolicy) Stub() *SentinelPolicyListStub {
	return &SentinelPolicyListStub{
		Name:             s.Name,
		Description:      s.Description,
		Scope:            s.Scope,
		EnforcementLevel: s.EnforcementLevel,
		Hash:             s.Hash,
		CreateIndex:      s.CreateIndex,
		ModifyIndex:      s.ModifyIndex,
	}
}

// SetHash is used to compute and set the hash of the ACL policy
func (s *SentinelPolicy) SetHash() []byte {
	// Initialize a 256bit Blake2 hash (32 bytes)
	hash, err := blake2b.New256(nil)
	if err != nil {
		panic(err)
	}

	// Write all the user set fields
	hash.Write([]byte(s.Name))
	hash.Write([]byte(s.Description))
	hash.Write([]byte(s.Scope))
	hash.Write([]byte(s.EnforcementLevel))
	hash.Write([]byte(s.Policy))

	// Finalize the hash
	hashVal := hash.Sum(nil)

	// Set and return the hash
	s.Hash = hashVal
	return hashVal
}

func (s *SentinelPolicy) Validate() error {
	var mErr multierror.Error
	if !validPolicyName.MatchString(s.Name) {
		err := fmt.Errorf("invalid name %q", s.Name)
		mErr.Errors = append(mErr.Errors, err)
	}
	if len(s.Description) > maxPolicyDescriptionLength {
		err := fmt.Errorf("description longer than %d", maxPolicyDescriptionLength)
		mErr.Errors = append(mErr.Errors, err)
	}
	if s.Scope != SentinelScopeSubmitJob {
		err := fmt.Errorf("invalid scope %q", s.Scope)
		mErr.Errors = append(mErr.Errors, err)
	}
	switch s.EnforcementLevel {
	case SentinelEnforcementLevelAdvisory, SentinelEnforcementLevelSoftMandatory, SentinelEnforcementLevelHardMandatory:
	default:
		err := fmt.Errorf("invalid enforcement level %q",
			s.EnforcementLevel)
		mErr.Errors = append(mErr.Errors, err)
	}

	// Validate that policy compiles
	if _, _, err := s.Compile(); err != nil {
		err = errwrap.Wrapf("policy compile error: {{err}}", err)
		mErr.Errors = append(mErr.Errors, err)
	}
	return mErr.ErrorOrNil()
}

// CacheKey returns a key that gets invalidated on changes
func (s *SentinelPolicy) CacheKey() string {
	return fmt.Sprintf("%s:%d", s.Name, s.ModifyIndex)
}

// Compile is used to compile the Sentinel policy for policy.SetPolicy
func (s *SentinelPolicy) Compile() (*ast.File, *token.FileSet, error) {
	// Parse
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, s.Name, s.Policy, 0)
	if err != nil {
		return nil, nil, err
	}

	// Perform semantic checks
	if err := semantic.Check(semantic.CheckOpts{
		File:    f,
		FileSet: fset,
	}); err != nil {
		return nil, nil, err
	}

	// Return the reuslt
	return f, fset, nil
}

// SentinelPolicyListRequest is used to request a list of policies
type SentinelPolicyListRequest struct {
	QueryOptions
}

// SentinelPolicySpecificRequest is used to query a specific policy
type SentinelPolicySpecificRequest struct {
	Name string
	QueryOptions
}

// SentinelPolicySetRequest is used to query a set of policies
type SentinelPolicySetRequest struct {
	Names []string
	QueryOptions
}

// SentinelPolicyListResponse is used for a list request
type SentinelPolicyListResponse struct {
	Policies []*SentinelPolicyListStub
	QueryMeta
}

// SingleSentinelPolicyResponse is used to return a single policy
type SingleSentinelPolicyResponse struct {
	Policy *SentinelPolicy
	QueryMeta
}

// SentinelPolicySetResponse is used to return a set of policies
type SentinelPolicySetResponse struct {
	Policies map[string]*SentinelPolicy
	QueryMeta
}

// SentinelPolicyDeleteRequest is used to delete a set of policies
type SentinelPolicyDeleteRequest struct {
	Names []string
	WriteRequest
}

// SentinelPolicyUpsertRequest is used to upsert a set of policies
type SentinelPolicyUpsertRequest struct {
	Policies []*SentinelPolicy
	WriteRequest
}

// QuotaSpec specifies the allowed resource usage across regions.
type QuotaSpec struct {
	// Name is the name for the quota object
	Name string

	// Description is an optional description for the quota object
	Description string

	// Limits is the set of quota limits encapsulated by this quota object. Each
	// limit applies quota in a particular region and in the future over a
	// particular priority range and datacenter set.
	Limits []*QuotaLimit

	// Hash is the hash of the object and is used to make replication efficient.
	Hash []byte

	// Raft indexes to track creation and modification
	CreateIndex uint64
	ModifyIndex uint64
}

// SetHash is used to compute and set the hash of the QuotaSpec
func (q *QuotaSpec) SetHash() []byte {
	// Initialize a 256bit Blake2 hash (32 bytes)
	hash, err := blake2b.New256(nil)
	if err != nil {
		panic(err)
	}

	// Write all the user set fields
	hash.Write([]byte(q.Name))
	hash.Write([]byte(q.Description))

	for _, l := range q.Limits {
		hash.Write(l.SetHash())
	}

	// Finalize the hash
	hashVal := hash.Sum(nil)

	// Set and return the hash
	q.Hash = hashVal
	return hashVal
}

func (q *QuotaSpec) Validate() error {
	var mErr multierror.Error
	if !validPolicyName.MatchString(q.Name) {
		err := fmt.Errorf("invalid name %q", q.Name)
		mErr.Errors = append(mErr.Errors, err)
	}
	if len(q.Description) > maxPolicyDescriptionLength {
		err := fmt.Errorf("description longer than %d", maxPolicyDescriptionLength)
		mErr.Errors = append(mErr.Errors, err)
	}

	if len(q.Limits) == 0 {
		err := fmt.Errorf("must provide at least one quota limit")
		mErr.Errors = append(mErr.Errors, err)
	} else {
		for i, l := range q.Limits {
			if err := l.Validate(); err != nil {
				wrapped := fmt.Errorf("invalid quota limit %d: %v", i, err)
				mErr.Errors = append(mErr.Errors, wrapped)
			}
		}
	}

	return mErr.ErrorOrNil()
}

// LimitsMap returns a lookup map of the quotas limits based on the limits hash
func (q *QuotaSpec) LimitsMap() map[string]*QuotaLimit {
	m := make(map[string]*QuotaLimit, len(q.Limits))
	for _, l := range q.Limits {
		m[string(l.Hash)] = l
	}
	return m
}

// Copy returns a copy of the QuotaSpec
func (q *QuotaSpec) Copy() *QuotaSpec {
	if q == nil {
		return nil
	}

	nq := new(QuotaSpec)
	*nq = *q

	// Copy the limits
	nq.Limits = make([]*QuotaLimit, 0, len(q.Limits))
	for _, limit := range q.Limits {
		nq.Limits = append(nq.Limits, limit.Copy())
	}

	// Copy the hash
	nq.Hash = make([]byte, len(q.Hash))
	copy(nq.Hash, q.Hash)

	return nq
}

// QuotaLimit describes the resource limit in a particular region.
type QuotaLimit struct {
	// Region is the region in which this limit has affect
	Region string

	// RegionLimit is the quota limit that applies to any allocation within a
	// referencing namespace in the region. A value of zero is treated as
	// unlimited and a negative value is treated as fully disallowed. This is
	// useful for once we support GPUs
	RegionLimit *Resources

	// Hash is the hash of the object and is used to make replication efficient.
	Hash []byte
}

// SetHash is used to compute and set the hash of the QuotaLimit
func (q *QuotaLimit) SetHash() []byte {
	// Initialize a 256bit Blake2 hash (32 bytes)
	hash, err := blake2b.New256(nil)
	if err != nil {
		panic(err)
	}

	// Write all the user set fields
	hash.Write([]byte(q.Region))

	if q.RegionLimit != nil {
		binary.Write(hash, binary.LittleEndian, int64(q.RegionLimit.CPU))
		binary.Write(hash, binary.LittleEndian, int64(q.RegionLimit.MemoryMB))

		// If the network region limit is not set, omit it from the hash for
		// backwards compatibility. Quotas are stored with their hashes
		mbits := 0
		if len(q.RegionLimit.Networks) > 0 {
			mbits = q.RegionLimit.Networks[0].MBits
		}
		if mbits > 0 {
			binary.Write(hash, binary.LittleEndian, int64(mbits))
		}
	}

	// Finalize the hash
	hashVal := hash.Sum(nil)
	q.Hash = hashVal
	return hashVal
}

// Validate validates the QuotaLimit
func (q *QuotaLimit) Validate() error {
	var mErr multierror.Error

	if q.Region == "" {
		err := fmt.Errorf("must provide a region")
		mErr.Errors = append(mErr.Errors, err)
	}

	if q.RegionLimit == nil {
		err := fmt.Errorf("must provide a region limit")
		mErr.Errors = append(mErr.Errors, err)
	} else {
		if q.RegionLimit.DiskMB != 0 {
			mErr.Errors = append(mErr.Errors, fmt.Errorf("quota can not limit disk"))
		}

		// limit Networks must have 1 element, which can only specify MBits
		if len(q.RegionLimit.Networks) > 1 {
			mErr.Errors = append(mErr.Errors, fmt.Errorf("only one network block per limit"))
		} else if len(q.RegionLimit.Networks) == 1 {
			n := q.RegionLimit.Networks[0]
			if !(n.Device == "" &&
				n.CIDR == "" &&
				n.IP == "" &&
				len(n.ReservedPorts) == 0 &&
				len(n.DynamicPorts) == 0) {
				mErr.Errors = append(mErr.Errors, fmt.Errorf("only network mbits may be limited"))
			}
		}
	}

	return mErr.ErrorOrNil()
}

// Copy returns a copy of the QuotaLimit
func (q *QuotaLimit) Copy() *QuotaLimit {
	if q == nil {
		return nil
	}

	nq := new(QuotaLimit)
	*nq = *q

	// Copy the limit
	nq.RegionLimit = q.RegionLimit.Copy()

	// Copy the hash
	nq.Hash = make([]byte, len(q.Hash))
	copy(nq.Hash, q.Hash)

	return nq
}

// Add adds the resources of the allocation to the QuotaLimit
func (q *QuotaLimit) Add(alloc *Allocation) {
	q.AddResource(alloc.Resources)
}

// Subtract removes the resources of the allocation to the QuotaLimit
func (q *QuotaLimit) Subtract(alloc *Allocation) {
	q.SubtractResource(alloc.Resources)
}

// AddResource adds the resources to the QuotaLimit metadata only copy. RegionLimit starts empty,
// the quota values are not retrieved until we call SuperSet
func (q *QuotaLimit) AddResource(r *Resources) {
	q.RegionLimit.CPU += r.CPU
	q.RegionLimit.MemoryMB += r.MemoryMB

	if len(r.Networks) == 0 {
		return
	}

	// If bits were claimed by this resource, add them
	bits := 0
	for _, n := range r.Networks {
		bits += n.MBits
	}

	if bits == 0 {
		return
	}

	if q.RegionLimit.Networks == nil {
		q.RegionLimit.Networks = Networks{{}}
	}
	q.RegionLimit.Networks[0].MBits += bits
}

// SubtractResource removes the resources to the QuotaLimit
func (q *QuotaLimit) SubtractResource(r *Resources) {
	q.RegionLimit.CPU -= r.CPU
	q.RegionLimit.MemoryMB -= r.MemoryMB

	if len(r.Networks) == 0 {
		return
	}

	if q.RegionLimit.Networks == nil {
		q.RegionLimit.Networks = Networks{{}}
	}

	bits := q.RegionLimit.Networks[0].MBits
	for _, n := range r.Networks {
		bits -= n.MBits
	}

	// If this limit was persisted without Networks, subtracting the bits specified in
	// the resource here will result in negative usage. (CPU and MemoryMB don't need
	// this check because they have always been specified in the limit and so have been
	// added already)
	if bits < 0 {
		bits = 0
	}

	q.RegionLimit.Networks[0].MBits = bits
}

// Superset returns if this quota is a super set of another. This is typically
// called where the method is called on the quota specication and the other
// object is the quota usage. The superset handles a limit being specified as -1
// to mean no usage allowed, zero meaning infinite usage allowed and anything
// greater to be the actual limit.
func (q *QuotaLimit) Superset(other *QuotaLimit) (bool, []string) {
	var exhausted []string
	r := q.RegionLimit
	or := other.RegionLimit

	if r.CPU < 0 && or.CPU > 0 {
		exhausted = append(exhausted, fmt.Sprintf("cpu exhausted (%d needed > 0 limit)", or.CPU))
	} else if r.CPU != 0 && r.CPU < or.CPU {
		exhausted = append(exhausted, fmt.Sprintf("cpu exhausted (%d needed > %d limit)", or.CPU, r.CPU))
	}

	if r.MemoryMB < 0 && or.MemoryMB > 0 {
		exhausted = append(exhausted, fmt.Sprintf("memory exhausted (%d needed > 0 limit)", or.MemoryMB))
	} else if r.MemoryMB != 0 && r.MemoryMB < or.MemoryMB {
		exhausted = append(exhausted, fmt.Sprintf("memory exhausted (%d needed > %d limit)", or.MemoryMB, r.MemoryMB))
	}

	if len(r.Networks) > 0 && len(or.Networks) > 0 && r.Networks[0].MBits != 0 {
		rmb := r.Networks[0].MBits
		omb := or.Networks[0].MBits
		if rmb < 0 {
			rmb = 0
		}
		if rmb < omb {
			exhausted = append(exhausted, fmt.Sprintf("network exhausted (%d needed > %d limit)", omb, rmb))
		}
	}

	return len(exhausted) == 0, exhausted
}

// QuotaUsage is local to a region and is used to track current
// resource usage for the quota object.
type QuotaUsage struct {
	// Name is a uniquely identifying name that is shared with the spec
	Name string

	// Used is the currently used resources for each quota limit. The map is
	// keyed by the QuotaLimit hash.
	Used map[string]*QuotaLimit

	// Raft indexes to track creation and modification
	CreateIndex uint64
	ModifyIndex uint64
}

func (q *QuotaUsage) MarshalJSON() ([]byte, error) {
	// Convert the raw string version of the hash into a base64 version. This
	// makes the map key match the hash when using JSON
	convertedMap := make(map[string]*QuotaLimit, len(q.Used))
	for _, limit := range q.Used {
		encoded := base64.StdEncoding.EncodeToString(limit.Hash)
		convertedMap[encoded] = limit
	}

	// The type alias allows us to only override the Used field
	type alias QuotaUsage
	return json.Marshal(&struct {
		Used map[string]*QuotaLimit
		*alias
	}{
		Used:  convertedMap,
		alias: (*alias)(q),
	})
}

func (q *QuotaUsage) UnmarshalJSON(data []byte) error {
	type Alias QuotaUsage
	aux := &struct{ *Alias }{Alias: (*Alias)(q)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	convertedMap := make(map[string]*QuotaLimit, len(aux.Used))
	for k, v := range aux.Used {
		decoded, err := base64.StdEncoding.DecodeString(k)
		if err != nil {
			return err
		}
		convertedMap[string(decoded)] = v
	}
	aux.Used = convertedMap

	return nil
}

// QuotaUsageFromSpec initializes a quota specification that can be used to
// track the usage for the specification.
func QuotaUsageFromSpec(spec *QuotaSpec) *QuotaUsage {
	return &QuotaUsage{
		Name: spec.Name,
		Used: make(map[string]*QuotaLimit, len(spec.Limits)),
	}
}

// DiffLimits returns the set of quota limits to create and destroy by comparing
// the hashes of the limits
func (q *QuotaUsage) DiffLimits(spec *QuotaSpec) (create, delete []*QuotaLimit) {
	if spec == nil && q == nil {
		// If both are nil, do nothing
		return nil, nil
	} else if q == nil {
		// If the usage is nil we add everything
		return spec.Limits, nil
	} else if spec == nil {
		// If there is no spec we delete everything
		delete = make([]*QuotaLimit, 0, len(q.Used))
		for _, l := range q.Used {
			delete = append(delete, l)
		}
		return nil, delete
	}

	// Determine what to add
	lookup := make(map[string]struct{}, len(spec.Limits))
	for _, l := range spec.Limits {
		hash := string(l.Hash)
		lookup[hash] = struct{}{}
		if _, ok := q.Used[hash]; !ok {
			create = append(create, l)
		}
	}

	// Determine what to delete
	for hash, used := range q.Used {
		if _, ok := lookup[hash]; !ok {
			delete = append(delete, used)
		}
	}

	return create, delete
}

// Copy returns a copy of the QuotaUsage
func (q *QuotaUsage) Copy() *QuotaUsage {
	if q == nil {
		return nil
	}

	nq := new(QuotaUsage)
	*nq = *q

	// Copy the usages
	nq.Used = make(map[string]*QuotaLimit, len(q.Used))
	for k, v := range q.Used {
		nq.Used[k] = v.Copy()
	}

	return nq
}

// QuotaSpecListRequest is used to request a list of quota specifications
type QuotaSpecListRequest struct {
	QueryOptions
}

// QuotaSpecSpecificRequest is used to query a specific quota specification
type QuotaSpecSpecificRequest struct {
	Name string
	QueryOptions
}

// QuotaSpecSetRequest is used to query a set of quota specs
type QuotaSpecSetRequest struct {
	Names []string
	QueryOptions
}

// QuotaSpecListResponse is used for a list request
type QuotaSpecListResponse struct {
	Quotas []*QuotaSpec
	QueryMeta
}

// SingleQuotaSpecResponse is used to return a single quota specification
type SingleQuotaSpecResponse struct {
	Quota *QuotaSpec
	QueryMeta
}

// QuotaSpecSetResponse is used to return a set of quota specifications
type QuotaSpecSetResponse struct {
	Quotas map[string]*QuotaSpec
	QueryMeta
}

// QuotaSpecDeleteRequest is used to delete a set of quota specifications
type QuotaSpecDeleteRequest struct {
	Names []string
	WriteRequest
}

// QuotaSpecUpsertRequest is used to upsert a set of quota specifications
type QuotaSpecUpsertRequest struct {
	Quotas []*QuotaSpec
	WriteRequest
}

// QuotaUsageListRequest is used to request a list of quota usages
type QuotaUsageListRequest struct {
	QueryOptions
}

// QuotaUsageSpecificRequest is used to query a specific quota usage
type QuotaUsageSpecificRequest struct {
	Name string
	QueryOptions
}

// QuotaUsageListResponse is used for a list request
type QuotaUsageListResponse struct {
	Usages []*QuotaUsage
	QueryMeta
}

// SingleQuotaUsageResponse is used to return a single quota usage
type SingleQuotaUsageResponse struct {
	Usage *QuotaUsage
	QueryMeta
}

func (m *Multiregion) Validate(jobType string, jobDatacenters []string) error {
	var mErr multierror.Error
	seen := map[string]struct{}{}
	for _, region := range m.Regions {
		if _, ok := seen[region.Name]; ok {
			mErr.Errors = append(mErr.Errors,
				fmt.Errorf("Multiregion region %q can't be listed twice",
					region.Name))
		}
		seen[region.Name] = struct{}{}
		if len(region.Datacenters) == 0 && len(jobDatacenters) == 0 {
			mErr.Errors = append(mErr.Errors,
				fmt.Errorf("Multiregion region %q must have at least 1 datacenter",
					region.Name),
			)
		}
	}
	if m.Strategy != nil {
		switch jobType {
		case JobTypeBatch:
			if m.Strategy.OnFailure != "" || m.Strategy.MaxParallel != 0 {
				mErr.Errors = append(mErr.Errors,
					errors.New("Multiregion batch jobs can't have an update strategy"))
			}
		case JobTypeSystem:
			if m.Strategy.OnFailure != "" {
				mErr.Errors = append(mErr.Errors,
					errors.New("Multiregion system jobs can't have an on_failure setting"))
			}
		default: // service
		}
	}
	return mErr.ErrorOrNil()
}

const (
	ScalingPolicyTypeVerticalCPU = "vertical_cpu"
	ScalingPolicyTypeVerticalMem = "vertical_mem"
)

func (p *ScalingPolicy) validateType() multierror.Error {
	var mErr multierror.Error

	// Check policy type and target
	switch p.Type {
	case ScalingPolicyTypeHorizontal:
		targetErr := p.validateTargetHorizontal()
		mErr.Errors = append(mErr.Errors, targetErr.Errors...)
	case ScalingPolicyTypeVerticalCPU, ScalingPolicyTypeVerticalMem:
		targetErr := p.validateTargetVertical()
		mErr.Errors = append(mErr.Errors, targetErr.Errors...)
	default:
		mErr.Errors = append(mErr.Errors, fmt.Errorf(`scaling policy type "%s" is not valid`, p.Type))
	}

	return mErr
}

func (p *ScalingPolicy) validateTargetVertical() (mErr multierror.Error) {
	if len(p.Target) == 0 {
		mErr.Errors = append(mErr.Errors, fmt.Errorf("%s policies must have a target", p.Type))
		return
	}

	// For a vertical policy, each target level can only be set if all
	// levels before it are also set, going, in order, from Namespace, Job,
	// Group and Task.
	// If a level is set, then all next levels must be empty. Otherwise we
	// do the same check for the next level.

	var (
		hasNamespace = p.Target[ScalingTargetNamespace] != ""
		hasJob       = p.Target[ScalingTargetJob] != ""
		hasGroup     = p.Target[ScalingTargetGroup] != ""
		hasTask      = p.Target[ScalingTargetTask] != ""
	)

	switch {
	case hasNamespace && !hasJob && !hasGroup && !hasTask:
		return
	case hasNamespace && hasJob && !hasGroup && !hasTask:
		return
	case hasNamespace && hasJob && hasGroup && !hasTask:
		return
	case hasNamespace && hasJob && hasGroup && hasTask:
		return
	}

	if !hasNamespace {
		mErr.Errors = append(mErr.Errors, fmt.Errorf("missing target namespace"))
		return
	}

	if !hasJob {
		mErr.Errors = append(mErr.Errors, fmt.Errorf("missing target job"))
		return
	}

	if !hasGroup {
		mErr.Errors = append(mErr.Errors, fmt.Errorf("missing target group"))
		return
	}

	return
}

// Return Task-specific scaling policies
func (j *Job) GetEntScalingPolicies() []*ScalingPolicy {
	policies := []*ScalingPolicy{}
	for _, tg := range j.TaskGroups {
		for _, t := range tg.Tasks {
			policies = append(policies, t.ScalingPolicies...)
		}
	}
	return policies
}

// Recommendation represents a recommended change to a job
type Recommendation struct {
	ID             string
	Region         string
	Namespace      string
	JobID          string
	JobVersion     uint64
	Group          string
	Task           string
	Resource       string
	Value          int
	Current        int
	Meta           map[string]interface{}
	Stats          map[string]float64
	EnforceVersion bool

	// SubmitTime is the time at which the job was submitted as a UnixNano in
	// UTC
	SubmitTime int64

	CreateIndex uint64
	ModifyIndex uint64
}

type RecommendationPath struct {
	Group    string
	Task     string
	Resource string
}

func (r *Recommendation) Validate() error {
	if r == nil {
		return nil
	}

	var mErr multierror.Error
	if r.Region == "" {
		err := fmt.Errorf("recommendation must specify the job region")
		mErr.Errors = append(mErr.Errors, err)
	}
	if r.Namespace == "" {
		err := fmt.Errorf("recommendation must specify a job namespace")
		mErr.Errors = append(mErr.Errors, err)
	}
	if r.JobID == "" {
		err := fmt.Errorf("recommendation must specify a job ID")
		mErr.Errors = append(mErr.Errors, err)
	}
	if r.Group == "" {
		err := fmt.Errorf("recommendation must contain a group")
		mErr.Errors = append(mErr.Errors, err)
	}
	if r.Task == "" {
		err := fmt.Errorf("recommendation must contain a task")
		mErr.Errors = append(mErr.Errors, err)
	}
	if r.Resource == "" {
		err := fmt.Errorf("recommendation must contain a resource")
		mErr.Errors = append(mErr.Errors, err)
	}
	minResources := MinResources()
	switch r.Resource {
	case "CPU":
		if r.Value < minResources.CPU {
			mErr.Errors = append(mErr.Errors, fmt.Errorf("minimum CPU value is %d; got %d", minResources.CPU, r.Value))
		}
	case "MemoryMB":
		if r.Value < minResources.MemoryMB {
			mErr.Errors = append(mErr.Errors, fmt.Errorf("minimum MemoryMB value is %d; got %d", minResources.MemoryMB, r.Value))
		}
	default:
		mErr.Errors = append(mErr.Errors, fmt.Errorf("resource not supported"))
	}
	return mErr.ErrorOrNil()
}

func (r *Recommendation) SamePath(r2 *Recommendation) bool {
	if r == nil && r2 == nil {
		return true
	}
	if r == nil || r2 == nil {
		return false
	}
	return r.Region == r2.Region &&
		r.JobID == r2.JobID &&
		r.Namespace == r2.Namespace &&
		r.Group == r2.Group &&
		r.Task == r2.Task &&
		r.Resource == r2.Resource
}

func (r *Recommendation) Copy() *Recommendation {
	if r == nil {
		return nil
	}
	c := &Recommendation{}
	*c = *r
	c.Meta = make(map[string]interface{})
	for k, v := range r.Meta {
		c.Meta[k] = v
	}
	c.Stats = make(map[string]float64)
	for k, v := range r.Stats {
		c.Stats[k] = v
	}
	return c
}

func (r *Recommendation) Target(group, task, resource string) {
	r.Group = group
	r.Task = task
	r.Resource = resource
}

func (r *Recommendation) UpdateJob(job *Job) error {
	if r == nil {
		return nil
	}
	if r.Namespace != job.Namespace {
		return fmt.Errorf("recommendation does not match job namespace")
	}
	if r.JobID != job.ID {
		return fmt.Errorf("recommendation does not match job ID")
	}
	group := job.LookupTaskGroup(r.Group)
	if group == nil {
		return fmt.Errorf("task group does not exist in job")
	}
	task := group.LookupTask(r.Task)
	if task == nil {
		return fmt.Errorf("task does not exist in group")
	}
	switch r.Resource {
	case "CPU":
		task.Resources.CPU = r.Value
	case "MemoryMB":
		task.Resources.MemoryMB = r.Value
	default:
		return fmt.Errorf("resource not valid")
	}
	return nil
}

// RecommendationDeleteRequest is used to delete a set of recommendations
type RecommendationDeleteRequest struct {
	Recommendations []string
	WriteRequest
}

// RecommendationApplyRequest is used to apply a set of recommendations
type RecommendationApplyRequest struct {
	Recommendations []string
	PolicyOverride  bool
	WriteRequest
}

type SingleRecommendationApplyResult struct {
	Namespace       string
	JobID           string
	JobModifyIndex  uint64
	EvalID          string
	EvalCreateIndex uint64
	Warnings        string
	Recommendations []string
}

type SingleRecommendationApplyError struct {
	Namespace       string
	JobID           string
	Recommendations []string
	Error           string
}

// RecommendationApplyResponse is the response from applying a set of recommendations
type RecommendationApplyResponse struct {
	UpdatedJobs []*SingleRecommendationApplyResult
	Errors      []*SingleRecommendationApplyError
	WriteMeta
}

// RecommendationUpsertRequest is used to upsert a recommendation
type RecommendationUpsertRequest struct {
	Recommendation *Recommendation
	WriteRequest
}

// RecommendationSpecificRequest is used to query a specific recommendation
type RecommendationSpecificRequest struct {
	RecommendationID string
	QueryOptions
}

// RecommendationListRequest is used to list the recommendations
type RecommendationListRequest struct {
	JobID string
	Group string
	Task  string
	QueryOptions
}

// SingleRecommendationResponse is used to return a single recommendation
type SingleRecommendationResponse struct {
	Recommendation *Recommendation
	QueryMeta
}

// RecommendationListResponse is used to return a list of recommendations
type RecommendationListResponse struct {
	Recommendations []*Recommendation
	QueryMeta
}
