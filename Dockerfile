FROM golang:1.25.7 AS builder

WORKDIR /src
ARG VERSION_TAG=""
ARG VERSION_COMMIT=""

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X github.com/dongwlin/nekomimi/internal/version.Tag=${VERSION_TAG} -X github.com/dongwlin/nekomimi/internal/version.Commit=${VERSION_COMMIT}" -o /out/nekomimi ./cmd/nekomimi

FROM alpine:latest AS runtime

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

COPY --from=builder /out/nekomimi /app/nekomimi

ENTRYPOINT ["/app/nekomimi"]
