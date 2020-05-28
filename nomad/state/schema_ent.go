// +build ent

package state

import memdb "github.com/hashicorp/go-memdb"

const (
	TableNamespaces       = "namespaces"
	TableSentinelPolicies = "sentinel_policy"
	TableQuotaSpec        = "quota_spec"
	TableQuotaUsage       = "quota_usage"
	TableLicense          = "license"
)

func init() {
	// Register premium schemas
	RegisterSchemaFactories([]SchemaFactory{
		sentinelPolicyTableSchema,
		quotaSpecTableSchema,
		quotaUsageTableSchema,
		namespaceTableSchema,
		licenseTableSchema,
	}...)
}

// namespaceTableSchema returns the MemDB schema for the namespace table.
func namespaceTableSchema() *memdb.TableSchema {
	return &memdb.TableSchema{
		Name: TableNamespaces,
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:         "id",
				AllowMissing: false,
				Unique:       true,
				Indexer: &memdb.StringFieldIndex{
					Field: "Name",
				},
			},
			"quota": {
				Name:         "quota",
				AllowMissing: true,
				Unique:       false,
				Indexer: &memdb.StringFieldIndex{
					Field: "Quota",
				},
			},
		},
	}
}

// sentinelPolicyTableSchema returns the MemDB schema for the sentinel policy
// table. This table is used to store the policies which are enforced.
func sentinelPolicyTableSchema() *memdb.TableSchema {
	return &memdb.TableSchema{
		Name: TableSentinelPolicies,
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:         "id",
				AllowMissing: false,
				Unique:       true,
				Indexer: &memdb.StringFieldIndex{
					Field: "Name",
				},
			},
			"scope": {
				Name:         "scope",
				AllowMissing: false,
				Unique:       false,
				Indexer: &memdb.StringFieldIndex{
					Field: "Scope",
				},
			},
		},
	}
}

// quotaSpecTableSchema returns the MemDB schema for the quota spec table. This
// table is used to store quota specifications.
func quotaSpecTableSchema() *memdb.TableSchema {
	return &memdb.TableSchema{
		Name: TableQuotaSpec,
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:         "id",
				AllowMissing: false,
				Unique:       true,
				Indexer: &memdb.StringFieldIndex{
					Field: "Name",
				},
			},
		},
	}
}

// quotaUsageTableSchema returns the MemDB schema for the quota usage table.
// This table is used to store quota usage rollups.
func quotaUsageTableSchema() *memdb.TableSchema {
	return &memdb.TableSchema{
		Name: TableQuotaUsage,
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:         "id",
				AllowMissing: false,
				Unique:       true,
				Indexer: &memdb.StringFieldIndex{
					Field: "Name",
				},
			},
		},
	}
}

// licenseTableSchema returns the MemDB schema for the license table.
// This table is used to store the enterprise license.
func licenseTableSchema() *memdb.TableSchema {
	return &memdb.TableSchema{
		Name: TableLicense,
		Indexes: map[string]*memdb.IndexSchema{
			"id": &memdb.IndexSchema{
				Name:         "id",
				AllowMissing: true,
				Unique:       true,
				Indexer: &memdb.ConditionalIndex{
					Conditional: func(obj interface{}) (bool, error) { return true, nil },
				},
			},
		},
	}
}
