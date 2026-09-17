FROM node:26-alpine AS web

WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf web/dist
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/arthurr0/backvault/internal/version.Version=${VERSION} -X github.com/arthurr0/backvault/internal/version.Commit=${COMMIT} -X github.com/arthurr0/backvault/internal/version.BuildDate=${BUILD_DATE}" \
    -o /out/backvault ./cmd/backvault

FROM debian:bookworm-slim

ARG MONGO_TOOLS_VERSION=100.18.0

ENV DEBIAN_FRONTEND=noninteractive

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl; \
    install -d -m 0755 /etc/apt/keyrings; \
    arch="$(dpkg --print-architecture)"; \
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc -o /etc/apt/keyrings/pgdg.asc; \
    echo "deb [arch=${arch} signed-by=/etc/apt/keyrings/pgdg.asc] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list; \
    curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc; \
    echo "deb [arch=${arch} signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      age \
      docker-ce-cli \
      gzip \
      mariadb-client \
      openssh-client \
      postgresql-client-18 \
      redis-tools \
      sqlite3 \
      tar \
      tzdata \
      zstd; \
    case "${arch}" in \
      amd64) mongo_tools="mongodb-database-tools-debian12-x86_64-${MONGO_TOOLS_VERSION}.deb" ;; \
      arm64) mongo_tools="mongodb-database-tools-ubuntu2204-arm64-${MONGO_TOOLS_VERSION}.deb" ;; \
      *) echo "unsupported architecture: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://fastdl.mongodb.org/tools/db/${mongo_tools}" -o /tmp/mongodb-database-tools.deb; \
    apt-get install -y --no-install-recommends /tmp/mongodb-database-tools.deb; \
    rm -f /tmp/mongodb-database-tools.deb; \
    rm -f /usr/bin/bsondump /usr/bin/mongoexport /usr/bin/mongofiles \
      /usr/bin/mongoimport /usr/bin/mongostat /usr/bin/mongotop; \
    apt-get clean; \
    rm -rf /var/lib/apt/lists/* /usr/share/doc/* /usr/share/man/*

RUN set -eux; \
    groupadd -g 1000 backvault; \
    useradd -u 1000 -g backvault -M -d /data -s /usr/sbin/nologin backvault; \
    mkdir -p /data; \
    chown -R backvault:backvault /data

COPY --from=build /out/backvault /usr/local/bin/backvault

ENV BACKVAULT_DATA_DIR=/data \
    BACKVAULT_LISTEN=:8080

USER backvault
WORKDIR /data
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["backvault"]
CMD ["serve"]
