# syntax=docker/dockerfile:1.7
#
# ai-adv sandbox image: runs generated code, its tests and the SAST tools
# (gosec, eslint + eslint-plugin-security) with no network. Everything a task
# may need is baked in at build time:
#   - Go 1.26 toolchain, gosec, the module cache for templates/go (gin & co);
#   - Node 22, node_modules for templates/vue (vue, vite, vitest, vue-tsc,
#     eslint, eslint-plugin-security, eslint-plugin-vue, typescript-eslint);
#   - sbx-run, the in-container entry point (copy /src -> /tmp/work, check the
#     dependencies against the templates, exec argv; no shell).
# The runtime flags (--network none, --read-only, --cap-drop ALL, non-root,
# limits) are set by the agent's docker runner, not here. No ENTRYPOINT: the
# runner always names /usr/local/bin/sbx-run explicitly.
#
# Build: make sandbox-image (Day14/agent) — the build needs the network, the
# image does not. A new dependency means editing a template and rebuilding.

ARG GO_IMAGE=golang:1.26-bookworm
ARG NODE_IMAGE=node:22-bookworm-slim
ARG BASE_IMAGE=debian:bookworm-slim

# ---------------------------------------------------------------- Go tools
FROM ${GO_IMAGE} AS gotools
ARG GOSEC_VERSION=v2.30.0
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
RUN for i in 1 2 3 4 5; do go install github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION} && break; \
      [ "$i" = 5 ] && exit 1; sleep 5; done \
 && go clean -cache -modcache

WORKDIR /build/sbx-run
COPY sbx-run/ ./
RUN go vet ./... && go build -o /out/sbx-run .

# Module cache for the Go template: every module its go.mod requires (direct
# and indirect), nothing else — `go mod download all` would also pull the
# test dependencies of every dependency (GBs). Then prove offline that the
# cache is complete by vetting and testing the selftest program.
WORKDIR /build/tpl-go
COPY templates/go/ ./
COPY selftest/go/ ./
ENV GOMODCACHE=/opt/gomodcache
RUN for i in 1 2 3 4 5; do \
      awk '/^require \($/{b=1;next} b&&/^\)/{b=0;next} b{print $1"@"$2} /^require [^(]/{print $2"@"$3}' go.mod \
        | xargs go mod download && break; \
      [ "$i" = 5 ] && exit 1; sleep 5; done \
 && GOPROXY=off GOFLAGS=-mod=readonly go vet ./... \
 && GOPROXY=off GOFLAGS=-mod=readonly go test ./... \
 && go clean -cache \
 && chmod -R a+rX,a-w /opt/gomodcache

# -------------------------------------------------------------- Node deps
FROM ${NODE_IMAGE} AS node
WORKDIR /opt/vue
COPY templates/vue/package.json templates/vue/package-lock.json ./
RUN npm ci --ignore-scripts --no-audit --no-fund \
 && npm cache clean --force

# ---------------------------------------------------------------- runtime
FROM ${BASE_IMAGE} AS runtime
COPY --from=gotools /usr/local/go /usr/local/go
COPY --from=gotools /go/bin/gosec /usr/local/bin/gosec
COPY --from=gotools /out/sbx-run /usr/local/bin/sbx-run
COPY --from=gotools /opt/gomodcache /opt/gomodcache
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
COPY --from=node /opt/vue /opt/vue
COPY templates/ /opt/sandbox/templates/
COPY eslint.security.mjs /opt/sandbox/eslint.security.mjs
# The eslint config imports its plugins by bare name: resolve them from the
# preinstalled tree. Everything under /opt is read-only for the sandbox user.
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s /opt/vue/node_modules /opt/sandbox/node_modules \
 && chmod -R a+rX,go-w /opt /usr/local/go \
 && rm -rf /var/lib/apt/lists/* /var/cache/*

ENV PATH=/opt/vue/node_modules/.bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin \
    CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOPROXY=off \
    GOFLAGS=-mod=readonly \
    GOMODCACHE=/opt/gomodcache \
    GOTELEMETRY=off \
    NPM_CONFIG_OFFLINE=true \
    NPM_CONFIG_IGNORE_SCRIPTS=true \
    NPM_CONFIG_UPDATE_NOTIFIER=false \
    HOME=/tmp

# Prebuilt build cache for the template's dependencies (gin & co, std for
# tests), read-only. sbx-run links it into the writable GOCACHE in /tmp, so a
# task compiles only its own packages. Built offline with the same env the
# sandbox uses, otherwise the cache keys would not match.
COPY templates/go/ /tmp/warm/
COPY selftest/go/ /tmp/warm/
RUN --network=none cd /tmp/warm \
 && GOCACHE=/opt/gocache go vet ./... \
 && GOCACHE=/opt/gocache go test -count=1 ./... \
 && GOCACHE=/opt/gocache go build ./... \
 && cd / && rm -rf /tmp/warm /opt/gocache/trim.txt \
 && chmod -R a+rX,go-w /opt/gocache

USER 65532:65532
WORKDIR /tmp

# --------------------------------------------------------------- selftest
# Proves, without network, that the baked-in dependencies are enough: Go
# template builds, tests and passes gosec; Vue template type-checks, tests and
# passes the security lint. The final stage depends on its marker file, so a
# broken toolchain fails the image build instead of the first agent run.
FROM runtime AS selftest
COPY --chown=65532:65532 templates/go/ /tmp/st/go/
COPY --chown=65532:65532 selftest/go/ /tmp/st/go/
COPY --chown=65532:65532 templates/vue/ /tmp/st/vue/
COPY --chown=65532:65532 selftest/vue/ /tmp/st/vue/
ENV GOCACHE=/tmp/gocache
RUN --network=none set -e; \
    sbx-run --stack go --src /tmp/st/go --work /tmp/w-go -- go vet ./... ; \
    sbx-run --stack go --src /tmp/st/go --work /tmp/w-go2 -- go test ./... ; \
    sbx-run --stack go --src /tmp/st/go --work /tmp/w-go3 -- gosec -quiet -fmt=json ./... > /tmp/gosec.json ; \
    sbx-run --stack vue --src /tmp/st/vue --work /tmp/w-vue -- vue-tsc --noEmit ; \
    sbx-run --stack vue --src /tmp/st/vue --work /tmp/w-vue2 -- vitest run ; \
    sbx-run --stack vue --src /tmp/st/vue --work /tmp/w-vue3 -- eslint -c /opt/sandbox/eslint.security.mjs -f json . > /tmp/eslint.json ; \
    touch /tmp/selftest.ok

# ------------------------------------------------------------------ final
FROM runtime
COPY --from=selftest /tmp/selftest.ok /opt/sandbox/selftest.ok
