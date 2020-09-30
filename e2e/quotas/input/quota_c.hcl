name        = "quotaC"
description = "Yet a third quota"

limit {
  region = "global"
  region_limit {
    cpu    = 300
    memory = 200
  }
}
