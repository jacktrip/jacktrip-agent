FROM golang:1.17 AS builder

ARG GIT_SHA=dev

WORKDIR /go/src/github.com/jacktrip/jacktrip-agent/

RUN apt-get update \
    && apt-get install --no-install-recommends -y libjack-jackd2-dev

COPY go.mod go.sum ./
COPY cmd ./cmd
COPY pkg ./pkg

RUN export ARCH=`dpkg --print-architecture` \
    && export GOARCH=`dpkg --print-architecture` \
    && if [ "x$GOARCH" = "xarmhf" ]; then export GOARCH=arm; fi \
    && GOOS=linux go build -ldflags "-X main.GitSHA=${GIT_SHA}" -o jacktrip-agent-$GOARCH ./cmd

FROM scratch AS artifact

COPY --from=builder /go/src/github.com/jacktrip/jacktrip-agent/jacktrip-agent-* /
