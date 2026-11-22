FROM golang:1.26.1-alpine@sha256:2389ebfa5b7f43eeafbd6be0c3700cc46690ef842ad962f6c5bd6be49ed82039 AS builder
WORKDIR /app/profile
ENV GOWORK=off
COPY --exclude=config/*.yaml . .
RUN --mount=type=cache,id=scooterflow-mod-cache,target=/go/pkg/mod \
    --mount=type=cache,id=scooterflow-build-cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o /profile ./cmd

FROM scratch AS release
WORKDIR /app
COPY --from=builder /profile /profile
COPY config/config.yaml /app/config/config.yaml
USER 65532:65532
EXPOSE 8081 9090
ENTRYPOINT ["/profile"]
