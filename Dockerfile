FROM golang:1.25.1-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/fms-backend .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /out/fms-backend ./fms-backend
RUN mkdir -p /app/data

# HTTP API (CapRover "Container HTTP Port") + FMC150 device TCP port.
# On the VPS the devices connect straight to DEVICE_PORT: add a CapRover Port Mapping
# 3101 -> 3101 and keep TUNNEL=0 (no ngrok/bore needed).
ENV APP_PORT=3308
ENV DEVICE_PORT=3101
ENV TUNNEL=0
ENV APP_DATA_DIR=/app/data
ENV TZ=Asia/Jakarta
EXPOSE 3308 3101

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:3308/health || exit 1

CMD ["./fms-backend"]
