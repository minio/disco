FROM golang:1.13

RUN apt-get update -y && apt-get install -y ca-certificates

ADD go.mod /go/src/github.com/minio/disco/go.mod
ADD go.sum /go/src/github.com/minio/disco/go.sum
WORKDIR /go/src/github.com/minio/disco/

# Get dependencies - will also be cached if we won't change mod/sum
RUN go mod download

ADD . /go/src/github.com/minio/disco/
WORKDIR /go/src/github.com/minio/disco/

ENV CGO_ENABLED=0


RUN go build -ldflags "-w -s" -a -o disco .

FROM scratch
MAINTAINER MinIO Development "dev@min.io"
EXPOSE 53

COPY --from=0 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=0 /go/src/github.com/minio/disco/disco .

CMD ["/disco"]
