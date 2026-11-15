FROM golang:1.27-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVICE=api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

FROM alpine:3.22

RUN adduser -D -u 10001 app
USER app
WORKDIR /app
COPY --from=builder /out/app /app/app

ENTRYPOINT ["/app/app"]
