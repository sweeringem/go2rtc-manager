FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/go2rtc-manager .

FROM alpine:3.21

WORKDIR /app

RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S app -G app

COPY --from=builder /out/go2rtc-manager /app/go2rtc-manager
COPY config.yaml /app/config.yaml
RUN mkdir -p /app/storage /config && chown -R app:app /app /config

ENV GO2RTC_MANAGER_HTTP_ADDR=:7181
EXPOSE 7181

USER app

ENTRYPOINT ["/app/go2rtc-manager"]
