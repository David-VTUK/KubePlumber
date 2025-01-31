FROM golang:1.23 as builder

ARG TARGETARCH

WORKDIR /app

COPY ./docker .

# Build the binary for the target platform
RUN GOOS=linux GOARCH=${TARGETARCH} go build -v -o kubeplumber-niccheck ./app/main.go

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/kubeplumber-niccheck .

ENTRYPOINT ["./kubeplumber-niccheck"]