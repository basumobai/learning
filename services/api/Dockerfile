FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY services/api/ .
RUN go mod download && go mod verify && CGO_ENABLED=0 go build -o /els ./cmd/api
FROM alpine:3.22
WORKDIR /app
COPY --from=builder /els /app/els
COPY migrations /app/migrations
EXPOSE 8080
CMD ["/app/els"]
