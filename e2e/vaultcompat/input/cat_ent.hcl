# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: BUSL-1.1

job "cat" {
  type      = "batch"
  namespace = "prod"

  group "testcase" {
    task "cat" {
      driver = "raw_exec"

      config {
        command = "cat"
        args    = ["${NOMAD_SECRETS_DIR}/vault_token"]
      }

      vault {
        namespace = "prod"
        policies  = ["default"]
      }
    }

    restart {
      attempts = 0
      mode     = "fail"
    }
  }
}
