# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

schema_version = 1

project {
  header_ignore = [
    "command/asset/*.hcl",
    "command/agent/bindata_assetfs.go",
    // Enterprise files do not fall under the open source licensing. OSS-ENT
    // merge conflicts might happen here, please be sure to put new OSS
    // exceptions above this comment.
    "**/*_ent.go",
    "**/*_ent_test.go",
    ".github/codeql/codeql-config.yml",
    "api/recommendations_test.go",
    "audit/*",
    "e2e/quotas/input/*",
    "**/quota_endpoint*.go",
    "**/sentinel_endpoint*.go",
    "nomad/license_default_*.go",
    "nomad/sentinel*",
    "nomad/structs/generate_ent.sh",
    "scheduler/scheduler_ent_shared.go",
    "scripts/enterprise/merge-oss.sh",
  ]
}
