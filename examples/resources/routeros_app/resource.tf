resource "routeros_app" "worker" {
  # Set false to begin asynchronous image download and startup.
  disabled  = true
  network   = "internal"
  use_https = false
  yaml = yamlencode({
    name = "example-worker"
    services = {
      worker = {
        image   = "docker.io/library/alpine:3.22"
        command = ["/bin/sh", "-c", "while true; do sleep 60; done"]
        environment = {
          ROUTER = "[networkRouterIP]"
        }
      }
    }
  })
}
