FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.work go.work.sum ./
COPY go/akb/go.mod go/akb/go.sum ./go/akb/

RUN cd go/akb && go mod download

COPY go/akb/ ./go/akb/

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /akb ./go/akb/cmd/stdio/

FROM alpine:3.21

RUN apk add --no-cache ca-certificates fuse3 rclone && \
    addgroup -S akb && adduser -S -G akb akb

COPY --from=builder /akb /usr/local/bin/akb

USER akb

ENTRYPOINT ["akb"]
