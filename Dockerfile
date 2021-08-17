FROM golang:1.16 AS builder

WORKDIR /go/src/github.com/jacktrip/jacktrip-agent

COPY go.mod go.sum ./
COPY cmd ./cmd
COPY pkg ./pkg

RUN GOOS=linux go build -o vs-server ./cmd

FROM debian:10.10-slim

RUN useradd -r -m -N -s /usr/sbin/nologin jacktrip \
	&& apt-get update \
	&& apt-get install --no-install-recommends -y ca-certificates=20200601~deb10u2 \
	&& apt-get clean autoclean autoremove \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /go/src/github.com/jacktrip/jacktrip-agent /usr/local/bin/jacktrip-agent

ENTRYPOINT ["/usr/local/bin/jacktrip-agent -o server"]
