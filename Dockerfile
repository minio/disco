FROM golang:1.14-alpine as builder

RUN apk add -U --no-cache ca-certificates git

ADD go.mod /go/src/github.com/minio/disco/go.mod
ADD go.sum /go/src/github.com/minio/disco/go.sum
WORKDIR /go/src/github.com/minio/disco/

# Get dependencies - will also be cached if we won't change mod/sum
RUN go mod download

ADD . /go/src/github.com/minio/disco/
WORKDIR /go/src/github.com/minio/disco/

ENV CGO_ENABLED=0

RUN go build -ldflags "-w -s -X main.version=$(git describe --tags --always --dirty)" -o disco

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/github.com/minio/disco/disco /disco

MAINTAINER MinIO Development "dev@min.io"
EXPOSE 53

ENTRYPOINT ["/disco"]
