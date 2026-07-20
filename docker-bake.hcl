variable "VERSION" {
  default = "0.1.0-dev"
}

group "default" {
  targets = ["controller", "maintainctl", "runnerd"]
}

group "runners" {
  targets = ["runner-base", "runner-python", "runner-node", "runner-c", "runner-cpp", "runner-rust", "runner-go", "runner-full"]
}

target "control-plane" {
  context    = "."
  dockerfile = "images/control-plane.Dockerfile"
  args = {
    VERSION = VERSION
  }
}

target "controller" {
  inherits = ["control-plane"]
  target   = "controller"
  tags     = ["local/code-maintainer-controller:${VERSION}"]
}

target "maintainctl" {
  inherits = ["control-plane"]
  target   = "maintainctl"
  tags     = ["local/code-maintainer-cli:${VERSION}"]
}

target "runnerd" {
  inherits = ["control-plane"]
  target   = "runnerd"
  tags     = ["local/code-maintainer-runnerd:${VERSION}"]
}

target "runner" {
  context    = "."
  dockerfile = "images/runners/Dockerfile"
}

target "runner-base" {
  inherits = ["runner"]
  target   = "runner-base"
  tags     = ["local/code-maintainer-runner-base:${VERSION}"]
}

target "runner-python" {
  inherits = ["runner"]
  target   = "runner-python"
  tags     = ["local/code-maintainer-runner-python:${VERSION}"]
}

target "runner-node" {
  inherits = ["runner"]
  target   = "runner-node"
  tags     = ["local/code-maintainer-runner-node:${VERSION}"]
}

target "runner-c" {
  inherits = ["runner"]
  target   = "runner-c"
  tags     = ["local/code-maintainer-runner-c:${VERSION}"]
}

target "runner-cpp" {
  inherits = ["runner"]
  target   = "runner-cpp"
  tags     = ["local/code-maintainer-runner-cpp:${VERSION}"]
}

target "runner-rust" {
  inherits = ["runner"]
  target   = "runner-rust"
  tags     = ["local/code-maintainer-runner-rust:${VERSION}"]
}

target "runner-go" {
  inherits = ["runner"]
  target   = "runner-go"
  tags     = ["local/code-maintainer-runner-go:${VERSION}"]
}

target "runner-full" {
  inherits = ["runner"]
  target   = "runner-full"
  tags     = ["local/code-maintainer-runner-full:${VERSION}"]
}
