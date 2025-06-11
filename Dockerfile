FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /jenkins-dashboard .

FROM alpine:latest

WORKDIR /app

COPY --from=builder /jenkins-dashboard .

COPY static ./static

EXPOSE 9090

CMD ["./jenkins-dashboard"]