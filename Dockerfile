FROM golang:1.22-alpine

WORKDIR /go/src/discovery
COPY . /go/src/discovery

EXPOSE 40003
EXPOSE 40008
EXPOSE 40009
RUN go install -v ./...
CMD ["discovery"]
