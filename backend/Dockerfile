FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/bootstrap-admin ./cmd/bootstrap-admin

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && \
    adduser -S -G app app

COPY --from=builder /out/api /usr/local/bin/api
COPY --from=builder /out/migrate /usr/local/bin/migrate
COPY --from=builder /out/bootstrap-admin /usr/local/bin/bootstrap-admin

USER app

EXPOSE 8080

CMD ["/usr/local/bin/api"]
