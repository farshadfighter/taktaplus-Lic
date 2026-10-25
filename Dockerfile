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

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/licserver"]
CMD ["serve"]
