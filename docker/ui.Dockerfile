# syntax=docker/dockerfile:1
#
# The bze-scan UI image. Build from the repository root:
#
#   docker build -f docker/ui.Dockerfile \
#     --secret id=build_env,src=ui/.env.dist \
#     --build-arg GIT_SHA=$(git rev-parse --short=8 HEAD) .
#
# build_env is a BuildKit secret: the env file `next build` reads as
# ui/.env.production. Its NEXT_PUBLIC_* values are inlined into the browser
# bundle, so they are public by definition; the secret mount only keeps the
# file out of the build context, the image layers and the build log. CI takes
# it from the deploy repository (<deploy dir>/build.env). Server-only
# values such as API_URL are read at run time from the container environment.
# The context is trimmed to ui/ by ui.Dockerfile.dockerignore.

FROM node:24-alpine AS build
ENV NEXT_TELEMETRY_DISABLED=1 COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /app

COPY ui/package.json ui/pnpm-lock.yaml ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY ui/ ./
# Output tracing copies .env.production into the standalone folder: drop it
# there. Its public values are already in the bundle; nothing else may ship.
RUN --mount=type=secret,id=build_env,target=/app/.env.production,required=true \
    pnpm build \
    && rm -f .next/standalone/.env.production

FROM node:24-alpine
ARG GIT_SHA=dev
LABEL org.opencontainers.image.source="https://github.com/bze-alphateam/bze-scan" \
      org.opencontainers.image.revision="${GIT_SHA}" \
      org.opencontainers.image.description="BZE block explorer UI" \
      org.opencontainers.image.licenses="MIT"

# tini as PID 1 forwards SIGTERM to node. Next exits with 143 after draining
# on SIGTERM; -e 143 reports that clean stop as 0.
RUN apk add --no-cache tini
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=3000

WORKDIR /app
# The standalone server and its pruned node_modules; static assets and
# public/ are not traced and are copied next to it. The server writes its
# cache under .next, hence the ownership.
COPY --from=build --chown=node:node /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public

USER node
EXPOSE 3000
ENTRYPOINT ["/sbin/tini", "-e", "143", "--"]
CMD ["node", "server.js"]
