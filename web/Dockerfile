FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod ./
COPY main.go ./

RUN go get github.com/xtls/xray-core@52a412d \
    github.com/google/uuid@v1.6.0 \
    github.com/skip2/go-qrcode@latest \
    && go mod tidy

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /templink-web main.go


FROM alpine:3.22

WORKDIR /app

COPY --from=builder /templink-web /app/templink-web

ENTRYPOINT ["/app/templink-web"]
