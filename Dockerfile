# --platform=$BUILDPLATFORM pins the builder to the host arch and cross-compiles
# via TARGETOS/TARGETARCH below, so multi-arch builds don't pay for a
# QEMU-emulated Go toolchain - only the final stage is emulated per-platform.
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/git-monitor ./cmd/git-monitor

# git-monitor shells out to `git` (over SSH) to poll the remote repository,
# so the runtime image needs git + openssh-client + CA certs - a fully
# static distroless/scratch image can't provide those.
FROM alpine:3.20
RUN apk add --no-cache git openssh-client ca-certificates \
    && addgroup -S -g 10001 git-monitor && adduser -S -u 10001 -G git-monitor git-monitor
COPY --from=build /out/git-monitor /usr/local/bin/git-monitor
# A numeric UID (not a name) is required for Kubernetes to verify
# runAsNonRoot without needing to resolve /etc/passwd inside the image -
# some container runtimes refuse to start the pod otherwise.
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/git-monitor"]
