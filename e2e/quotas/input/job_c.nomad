job "namespace_c" {

  namespace = "NamespaceC"

  datacenters = ["dc1", "dc2"]

  constraint {
    attribute = "${attr.kernel.name}"
    value     = "linux"
  }

  group "group" {

    count = 1

    task "task" {

      driver = "raw_exec"

      config {
        command = "/bin/sh"
        args    = ["-c", "sleep 300"]
      }

      # resources so that a smaller count of jobs
      # will fit within the quota
      resources {
        cpu    = 256
        memory = 128
      }
    }
  }
}
