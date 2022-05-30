FROM golang:1.18-alpine3.16 as builder

WORKDIR /src

COPY go.mod /src/go.mod
COPY client.go /src/client.go

RUN apk add --no-cache ca-certificates

RUN CGO_ENABLED=0 GOOS=linux go build

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /src/client /bin/client

ENTRYPOINT ["/bin/client"]
