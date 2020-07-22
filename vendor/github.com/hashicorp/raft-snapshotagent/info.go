package snapshotagent

import "strings"

func SampleConfig(product string) string {
	sample := `
snapshot {
  interval         = "1h"
  retain           = 30
  stale            = false
  service          = "__PRODUCT__-snapshot"
  deregister_after = "72h"
  lock_key         = "__PRODUCT__-snapshot/lock"
  max_failures     = 3
  prefix           = "__PRODUCT__"
}

log {
  level           = "INFO"
  enable_syslog   = false
  syslog_facility = "LOCAL0"
}

consul {
  enabled         = true
  http_addr       = "127.0.0.1:8500"
  token           = ""
  datacenter      = ""
  ca_file         = ""
  ca_path         = ""
  cert_file       = ""
  key_file        = ""
  tls_server_name = ""
}

# one storage block is required

local_storage {
  path = "."
}

aws_storage {
  access_key_id     = ""
  secret_access_key = ""
  s3_region         = ""
  s3_endpoint       = ""
  s3_bucket         = ""
  s3_key_prefix     = "__PRODUCT__-snapshot"
}

azure_blob_storage {
  account_name   = ""
  account_key    = ""
  container_name = ""
}

google_storage {
  bucket = ""
}
`
	return strings.ReplaceAll(sample, "__PRODUCT__", product)
}
