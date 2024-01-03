# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

schema_version = 1

project {
  license        = "BUSL-1.1"
  copyright_year = 2024

  header_ignore = [
    "command/asset/*.hcl",
    "command/agent/bindata_assetfs.go",
    "ui/node_modules",

    // Enterprise files do not fall under the open source licensing. CE-ENT
    // merge conflicts might happen here, please be sure to put new CE
    // exceptions above this comment.
    "**/*_ent.go",
    "**/*_ent_test.go",
    "audit/*.go",
    ".github/codeql/codeql-config.yml",
    "command/agent/quota_endpoint.go",
    "command/agent/sentinel_endpoint.go",
    "command/agent/quota_endpoint_test.go",
    "command/agent/sentinel_endpoint_test.go",
    "e2e/metrics/scripts/metric_runner.sh",
    "e2e/metrics/scripts/circleci-metrics.yaml",
    "e2e/quotas/**",
    "nomad/license_default_on_prem_modules.go",
    "nomad/license_default_on_prem_plat.go",
    "nomad/sentinel.go",
    "nomad/sentinel_test.go",
    "nomad/sentinel_endpoint.go",
    "nomad/quota_endpoint.go",
    "nomad/sentinel_endpoint_test.go",
    "nomad/quota_endpoint_test.go",
    "scheduler/scheduler_ent_shared.go",
    "scripts/enterprise/merge-ce.sh",
    "scripts/release/circleci-release.yaml",
    "nomad/structs/generate_ent.sh",
  ]
}
