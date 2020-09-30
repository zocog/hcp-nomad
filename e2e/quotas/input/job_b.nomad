job "namespace_b" {

  namespace = "NamespaceB"

  datacenters = ["dc1", "dc2"]

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {

    count = 2

    task "task" {

      driver = "raw_exec"

      config {
        command = "/bin/sh"
        args    = ["-c", "sleep 300"]
      }

      # resources for a single group are smaller than quota,
      # but for more than one we won't fit
      resources {
        cpu    = 256
        memory = 128
      }

      env {
        # give us something to force re-placement
        TEST = "X"
      }
    }
  }
}
