FROM golang:1.21-alpine AS builder
WORKDIR /peer-messenger
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY ./ ./
RUN CGO_ENABLED=0 go build -o /bin/app

FROM alpine:latest
RUN apk --update add ca-certificates
COPY --from=builder /bin/app /bin/app
EXPOSE 8080
EXPOSE 9090

CMD [ "/bin/app" ]