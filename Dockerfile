FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/app ./web/dist/app
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
RUN LDFLAGS="-s -w -X github.com/farshadfighter/taktaplus-lic/internal/buildinfo.Version=${VERSION} \
      -X github.com/farshadfighter/taktaplus-lic/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/farshadfighter/taktaplus-lic/internal/buildinfo.Date=${DATE}" && \
    CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/licserver ./cmd/licserver && \
    CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o /out/lictool ./cmd/lictool
# The data directory must belong to the nonroot user so a fresh named volume
# inherits a writable owner.
RUN mkdir -p /data-root/var/lib/taktaplus-lic

# Alpine rather than distroless: self-backups use the PostgreSQL client tools
# and the upgrade scripts check health with wget.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates postgresql16-client && \
    addgroup -S -g 65532 nonroot && adduser -S -D -H -u 65532 -G nonroot nonroot
COPY --from=build /out/ /usr/local/bin/
COPY --from=build --chown=65532:65532 /data-root/ /
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/licserver"]
CMD ["serve"]
