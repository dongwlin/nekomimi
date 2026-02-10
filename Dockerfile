FROM golang:1.25.7 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /out/nekomimi ./cmd/nekomimi

FROM alpine:latest AS runtime

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

COPY --from=builder /out/nekomimi /app/nekomimi

ENTRYPOINT ["/app/nekomimi"]
