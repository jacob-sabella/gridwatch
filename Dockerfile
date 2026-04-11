# ---- build stage ----
FROM golang:1.23-alpine AS build

WORKDIR /src

# Pre-download deps in their own layer so source-only edits don't bust
# the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# CGO_ENABLED=0 + -trimpath produces a fully static binary that runs
# on distroless/static. tzdata is bundled via _ "time/tzdata" import.
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
      -trimpath \
      -ldflags="-s -w \
        -X github.com/jacob-sabella/gridwatch/internal/buildinfo.Version=${VERSION} \
        -X github.com/jacob-sabella/gridwatch/internal/buildinfo.Commit=${COMMIT} \
        -X github.com/jacob-sabella/gridwatch/internal/buildinfo.Date=${DATE}" \
      -o /out/gridwatch \
      ./cmd/gridwatch

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="gridwatch"
LABEL org.opencontainers.image.description="Self-hosted esports TV guide"
LABEL org.opencontainers.image.source="https://github.com/jacob-sabella/gridwatch"
LABEL org.opencontainers.image.licenses="MIT"

COPY --from=build /out/gridwatch /gridwatch
COPY configs/gridwatch.example.yaml /etc/gridwatch/gridwatch.yaml

USER nonroot:nonroot
EXPOSE 8080
VOLUME ["/data"]

ENTRYPOINT ["/gridwatch"]
CMD ["--config", "/etc/gridwatch/gridwatch.yaml"]
