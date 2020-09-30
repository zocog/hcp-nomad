name        = "quotaA"
description = "A quota"

limit {
  region = "global"
  region_limit {
    cpu    = 1000
    memory = 300
  }
}
