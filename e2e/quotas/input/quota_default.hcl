// TODO: we can't apply quotas to the default namespace without
// permanently changing the test environment:
// https://github.com/hashicorp/nomad-enterprise/issues/414
name        = "default-quota"
description = "Unlimited default quota"

limit {
  region = "global"
  region_limit {}
}
