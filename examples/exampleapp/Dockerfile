# syntax=docker/dockerfile:1
FROM golang:1.17-alpine3.14 as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /exampleapp

FROM alpine:3.14 as runner

WORKDIR /

COPY --from=builder /exampleapp /exampleapp

EXPOSE 2112

CMD [ "/exampleapp" ]