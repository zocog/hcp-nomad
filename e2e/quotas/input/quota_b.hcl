name        = "quotaB"
description = "Some other quota"

limit {
  region = "global"
  region_limit {
    cpu    = 300
    memory = 300
  }
}

limit {
  # this region doesn't exist, but we should not get an
  # error applying this spec or be restricted by it
  region = "invalid"
  region_limit {
    cpu    = 1
    memory = 1
  }
}
