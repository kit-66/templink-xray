FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /templink-web main.go


FROM alpine:3.22

WORKDIR /app

COPY --from=builder /templink-web /app/templink-web

ENTRYPOINT ["/app/templink-web"]
