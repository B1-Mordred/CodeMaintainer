variable "VERSION" {
  default = "0.1.0-dev"
}

group "default" {
  targets = ["controller", "maintainctl", "runnerd", "fake-model-server", "git-bridge", "inference"]
}

group "runners" {
  targets = ["runner-base", "runner-python", "runner-node", "runner-c", "runner-cpp", "runner-rust", "runner-go", "runner-full", "implementation-agent", "qc-agent", "dependencies-worker"]
}

target "dependencies-worker" {
  context = "."
  dockerfile = "images/runners/Dockerfile"
  target = "dependencies-worker"
  tags = ["local/codemaintainer-dependencies-worker:0.1.0-dev"]
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
  tags     = ["local/codemaintainer-controller:${VERSION}"]
}

target "maintainctl" {
  inherits = ["control-plane"]
  target   = "maintainctl"
  tags     = ["local/codemaintainer-cli:${VERSION}"]
}

target "runnerd" {
  inherits = ["control-plane"]
  target   = "runnerd"
  tags     = ["local/codemaintainer-runnerd:${VERSION}"]
}

target "fake-model-server" {
  inherits = ["control-plane"]
  target   = "fake-model-server"
  tags     = ["local/codemaintainer-fake-model-server:${VERSION}"]
}

target "git-bridge" {
  inherits = ["control-plane"]
  target   = "git-bridge"
  tags     = ["local/codemaintainer-git-bridge:${VERSION}"]
}

target "inference" {
  context    = "."
  dockerfile = "images/inference/Dockerfile"
  target     = "inference-haswell"
  tags       = ["local/codemaintainer-inference:${VERSION}"]
}

target "runner" {
  context    = "."
  dockerfile = "images/runners/Dockerfile"
}

target "runner-base" {
  inherits = ["runner"]
  target   = "runner-base"
  tags     = ["local/codemaintainer-runner-base:${VERSION}"]
}

target "runner-python" {
  inherits = ["runner"]
  target   = "runner-python"
  tags     = ["local/codemaintainer-runner-python:${VERSION}"]
}

target "runner-node" {
  inherits = ["runner"]
  target   = "runner-node"
  tags     = ["local/codemaintainer-runner-node:${VERSION}"]
}

target "runner-c" {
  inherits = ["runner"]
  target   = "runner-c"
  tags     = ["local/codemaintainer-runner-c:${VERSION}"]
}

target "runner-cpp" {
  inherits = ["runner"]
  target   = "runner-cpp"
  tags     = ["local/codemaintainer-runner-cpp:${VERSION}"]
}

target "runner-rust" {
  inherits = ["runner"]
  target   = "runner-rust"
  tags     = ["local/codemaintainer-runner-rust:${VERSION}"]
}

target "runner-go" {
  inherits = ["runner"]
  target   = "runner-go"
  tags     = ["local/codemaintainer-runner-go:${VERSION}"]
}

target "runner-full" {
  inherits = ["runner"]
  target   = "runner-full"
  tags     = ["local/codemaintainer-runner-full:${VERSION}"]
}

target "implementation-agent" {
  inherits = ["runner"]
  target   = "implementation-agent"
  tags     = ["local/codemaintainer-implementation-agent:${VERSION}"]
}

target "qc-agent" {
  inherits = ["runner"]
  target   = "qc-agent"
  tags     = ["local/codemaintainer-qc-agent:${VERSION}"]
}
