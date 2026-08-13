# syntax=docker/dockerfile:1

FROM golang:1.26.5-alpine AS builder

RUN apk add --no-cache gcc musl-dev linux-headers git

COPY --from=go-qrl-source . /go-qrl
COPY . /qrl-tests

RUN cd /go-qrl && go run build/ci.go install -static ./cmd/clef
RUN cd /qrl-tests && \
    GOWORK=off go mod edit -replace github.com/theQRL/go-qrl=/go-qrl && \
    GOWORK=off go build -o /go-qrl/build/bin/clefauto ./cmd/clefauto

FROM alpine:latest

RUN apk add --no-cache ca-certificates
COPY --from=builder /go-qrl/build/bin/clef /usr/local/bin/clef-bin
COPY --from=builder /go-qrl/build/bin/clefauto /usr/local/bin/clef
