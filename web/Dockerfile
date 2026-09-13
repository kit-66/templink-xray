FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod ./
RUN go mod tidy

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /templink-web main.go


FROM alpine:3.22

WORKDIR /app

COPY --from=builder /templink-web /app/templink-web

ENTRYPOINT ["/app/templink-web"]
