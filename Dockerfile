FROM golang:1-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o echohawk main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/echohawk .
COPY .env .
CMD ["./echohawk"]