FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /cheetah-gateway .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /cheetah-gateway /usr/local/bin/cheetah-gateway
EXPOSE 3000
CMD ["cheetah-gateway"]
