# syntax=docker/dockerfile:1
#
# The bze-scan backend image. Build from the repository root:
#
#   docker build -f docker/backend.Dockerfile --build-arg GIT_SHA=$(git rev-parse --short=8 HEAD) .
#
# The image holds the binary only: no configuration, no secrets. Everything
# comes from the environment at run time (see backend/.env.dist).

# The golang image pins GOTOOLCHAIN=local; auto lets go fetch the exact
# toolchain backend/go.mod asks for when this base is older.
FROM golang:1.26-alpine AS build
ENV GOTOOLCHAIN=auto CGO_ENABLED=0
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
ARG GIT_SHA=dev
RUN go build -trimpath -ldflags "-s -w -X main.version=${GIT_SHA}" -o /out/bze-scan ./cmd/bze-scan

# Alpine rather than distroless so compose can healthcheck /health with
# busybox wget; the CA bundle is already in the base image.
FROM alpine:3.22
ARG GIT_SHA=dev
LABEL org.opencontainers.image.source="https://github.com/bze-alphateam/bze-scan" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.description="BZE block explorer backend" \
      org.opencontainers.image.licenses="MIT"

RUN addgroup -S -g 10001 bze && adduser -S -D -H -u 10001 -G bze bze
COPY --from=build /out/bze-scan /usr/local/bin/bze-scan

USER 10001:10001
EXPOSE 8080
# serve by default; one-shots replace the command:
#   docker compose run --rm backend migrate up
ENTRYPOINT ["/usr/local/bin/bze-scan"]
CMD ["serve"]
