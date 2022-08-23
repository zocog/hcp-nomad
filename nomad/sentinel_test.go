//go:build ent
// +build ent

package nomad

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"

	multierror "github.com/hashicorp/go-multierror"
	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/nomad/mock"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/testutil"
	"github.com/hashicorp/sentinel/sentinel"
	"github.com/stretchr/testify/assert"
)

func TestServer_Sentinel_EnforceScope(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// data call back for enforcement
	dataCB := func() map[string]interface{} {
		return map[string]interface{}{
			"foo_bar": false,
		}
	}

	// Create a fake policy
	policy1 := mock.SentinelPolicy()
	policy2 := mock.SentinelPolicy()
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy1, policy2})

	// Check that everything passes
	warn, err := s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	assert.Nil(t, err)
	assert.Nil(t, warn)

	// Add a failing policy
	policy3 := mock.SentinelPolicy()
	policy3.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	policy3.Policy = "main = rule { foo_bar }"
	s1.State().UpsertSentinelPolicies(1001, []*structs.SentinelPolicy{policy3})

	// Check that we get an error
	warn, err = s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	assert.NotNil(t, err)
	assert.Nil(t, warn)

	// Update policy3 to be advisory
	p3update := new(structs.SentinelPolicy)
	*p3update = *policy3
	p3update.EnforcementLevel = structs.SentinelEnforcementLevelAdvisory
	s1.State().UpsertSentinelPolicies(1002, []*structs.SentinelPolicy{p3update})

	// Check that we get a warning
	warn, err = s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	assert.Nil(t, err)
	assert.NotNil(t, warn)
}

func TestServer_Sentinel_EnforceScope_MultiJobPolicies(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// data call back for enforement
	job := mock.Job()
	dataCB := func() map[string]interface{} {
		return map[string]interface{}{
			"job": job,
		}
	}

	// Create a fake policy
	p1 := mock.SentinelPolicy()
	p1.Policy = "main = rule { job.priority == 50 }"
	p2 := mock.SentinelPolicy()
	p2.Policy = "main = rule { job.region == \"global\" }"
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{p1, p2})

	// Check that everything passes
	warn, err := s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	if !assert.Nil(t, err) {
		t.Logf("error: %v", err)
	}
	if !assert.Nil(t, warn) {
		t.Logf("warn: %v", warn)
	}
}

// Ensure the standard lib is present
func TestServer_Sentinel_EnforceScope_Stdlib(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// data call back for enforement
	dataCB := func() map[string]interface{} {
		return map[string]interface{}{
			"my_string": "foobar",
		}
	}

	// Add a policy
	policy1 := mock.SentinelPolicy()
	policy1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	policy1.Policy = `import "strings"
	main = rule { strings.has_prefix(my_string, "foo") }
	`
	s1.State().UpsertSentinelPolicies(1001, []*structs.SentinelPolicy{policy1})

	// Check that we pass
	warn, err := s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	assert.Nil(t, err)
	assert.Nil(t, warn)
}

// Ensure HTTP import is not present
func TestServer_Sentinel_EnforceScope_HTTP(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// data call back for enforement
	dataCB := func() map[string]interface{} {
		return map[string]interface{}{
			"my_string": "foobar",
		}
	}

	// Add a policy
	policy1 := mock.SentinelPolicy()
	policy1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	policy1.Policy = `import "http"
	main = rule { true }
	`
	s1.State().UpsertSentinelPolicies(1001, []*structs.SentinelPolicy{policy1})

	// Check that we get an error
	warn, err := s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)
	assert.NotNil(t, err)
	assert.Nil(t, warn)
}

func TestServer_SentinelPoliciesByScope(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// Create a fake policy
	policy1 := mock.SentinelPolicy()
	policy2 := mock.SentinelPolicy()
	s1.State().UpsertSentinelPolicies(1000,
		[]*structs.SentinelPolicy{policy1, policy2})

	// Ensure we get them back
	ps, err := s1.sentinelPoliciesByScope(structs.SentinelScopeSubmitJob)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(ps))
}

func TestServer_PrepareSentinelPolicies(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	// Create a fake policy
	policy1 := mock.SentinelPolicy()
	policy2 := mock.SentinelPolicy()
	in := []*structs.SentinelPolicy{policy1, policy2}

	// Test compilation
	prep, err := prepareSentinelPolicies(s1.sentinel, in)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(prep))
}

func TestSentinelResultToWarnErr(t *testing.T) {
	sent := sentinel.New(nil)

	// Setup three policies:
	// p1: Fails, Hard-mandatory (err)
	// p2: Fails, Soft-mandatory + Override (warn)
	// p3: Fails, Advisory (warn)
	p1 := mock.SentinelPolicy()
	p1.EnforcementLevel = structs.SentinelEnforcementLevelHardMandatory
	p1.Policy = "main = rule { false }"

	p2 := mock.SentinelPolicy()
	p2.EnforcementLevel = structs.SentinelEnforcementLevelSoftMandatory
	p2.Policy = "main = rule { false }"

	p3 := mock.SentinelPolicy()
	p3.EnforcementLevel = structs.SentinelEnforcementLevelAdvisory
	p3.Policy = "main = rule { false }"

	// Prepare the policies
	ps := []*structs.SentinelPolicy{p1, p2, p3}
	prep, err := prepareSentinelPolicies(sent, ps)
	assert.Nil(t, err)

	// Evaluate with an override
	result := sent.Eval(prep, &sentinel.EvalOpts{
		Override: true,
		EvalAll:  true, // For testing
	})

	// Get the errors
	warn, err := sentinelResultToWarnErr(result)
	assert.NotNil(t, err)
	assert.NotNil(t, warn)

	// Check the lengths
	assert.Equal(t, 1, len(err.(*multierror.Error).Errors))
	assert.Equal(t, 2, len(warn.(*multierror.Error).Errors))
}

func TestServer_Sentinel_Extensions(t *testing.T) {
	ci.Parallel(t)
	s1, _, cleanupS1 := TestACLServer(t, nil)
	defer cleanupS1()
	testutil.WaitForLeader(t, s1.RPC)

	type testCase struct {
		name      string
		policy    string
		warnOrErr bool
		userList  []string
		policies  []string
		errMsg    string
	}

	var testCases = []testCase{
		{
			name:      "valid-token",
			policy:    tokenPolicy,
			warnOrErr: false,
			userList:  []string{},
			policies:  []string{"foo"},
			errMsg:    "",
		},
		{
			name:      "invalid-token",
			policy:    tokenPolicy,
			warnOrErr: true,
			userList:  []string{},
			policies:  []string{"baz"},
			errMsg:    "does not have the correct policy",
		},
		{
			name:      "valid-namespace",
			policy:    nsPolicy,
			warnOrErr: false,
			userList:  []string{"bob", "suzy"},
			policies:  []string{"foo"},
			errMsg:    "",
		},
		{
			name:      "invalid-namespace",
			policy:    nsPolicy,
			warnOrErr: true,
			userList:  []string{"suzy"},
			policies:  []string{"foo"},
			errMsg:    "not permitted on namespace",
		},
		{
			name:      "valid-combined",
			policy:    combinedPolicy,
			warnOrErr: false,
			userList:  []string{"bob"},
			policies:  []string{"foo"},
			errMsg:    "",
		},
		{
			name:      "invalid-combined-token",
			policy:    combinedPolicy,
			warnOrErr: true,
			userList:  []string{"bob"},
			policies:  []string{"baz"},
			errMsg:    "does not have the correct policy",
		},
		{
			name:      "invalid-combined-namespace",
			policy:    combinedPolicy,
			warnOrErr: true,
			userList:  []string{"suzy"},
			policies:  []string{"foo"},
			errMsg:    "not permitted on namespace",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job := mock.Job()
			ns := mock.Namespace()
			job.Namespace = ns.Name
			token := mock.ACLToken()
			job.NomadTokenID = token.SecretID

			// data call back for enforcement
			dataCB := func() map[string]interface{} {
				return map[string]interface{}{
					"job":             job,
					"namespace":       ns,
					"nomad_acl_token": token,
				}
			}

			if len(tc.userList) > 0 {
				ns.Meta["userlist"] = fmt.Sprintf("[%s]", strings.Join(tc.userList, ", "))

				for _, tg := range job.TaskGroups {
					for _, t := range tg.Tasks {
						// Only bob is valid
						t.User = "bob"
					}
				}
			}

			token.Policies = []string{}
			if len(tc.policies) > 0 {
				token.Policies = tc.policies
			}

			policy := mock.SentinelPolicy()
			policy.Policy = tc.policy

			s1.State().UpsertSentinelPolicies(1000,
				[]*structs.SentinelPolicy{policy})

			// Check that everything as expected
			warn, _ := s1.enforceScope(false, structs.SentinelScopeSubmitJob, dataCB)

			if tc.warnOrErr {
				require.NotNil(t, warn)
				require.Contains(t, warn.Error(), tc.errMsg)
			} else {
				require.Nil(t, warn)
			}
		})
	}
}

const nsPolicy = `
user_check = func(taskgroups) {
    result = true

    for taskgroups as tg {
        for tg.tasks as t {
            if namespace.meta.userlist contains t.user {
                print("The correct user for the", t.name, "task is being used")
            } else {
                print("The task,",t.name," has user \" ",t.user,"\" that is not permitted on namespace \"",namespace.name,"\", please try one of: ", namespace.meta.userlist)

                result = false
                break
            }           
        }
    }

    return result
}

main = rule {
  user_check(job.task_groups)
}
`

const tokenPolicy = `
token_check = func(taskgroups) {
    result = true

    for taskgroups as tg {
        for tg.tasks as t {
            if nomad_acl_token.policies contains "foo" {
                print("The correct policy for", t.name, "task is being used")
            } else {
                print("The task" ,t.name,"does not have the correct policy")

                result = false
                break
            }           
        }
    }

    return result
}

main = rule {
  token_check(job.task_groups)
}
`

const combinedPolicy = `
user_check = func(taskgroups) {
    result = true

    for taskgroups as tg {
        for tg.tasks as t {
            if namespace.meta.userlist contains t.user {
                print("The correct user for the ", t.name, " task is being used")
            } else {
                print("The task,",t.name,"has user,\" ",t.user,"\" that is not permitted on namespace \"",namespace.name,"\", please try one of: ", namespace.meta.userlist)

                result = false
                break
            }           
        }
    }

    return result
}

token_check = func(taskgroups) {
    result = true

    for taskgroups as tg {
        for tg.tasks as t {
            if nomad_acl_token.policies contains "foo" {
                print("The correct policy for", t.name, "task is being used")
            } else {
                print("The task" ,t.name,"does not have the correct policy")

                result = false
                break
            }           
        }
    }

    return result
}

main = rule {
  user_check(job.task_groups) and token_check(job.task_groups)
}
`
