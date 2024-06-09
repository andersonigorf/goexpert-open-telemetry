FROM golang:1.22.4-alpine3.20 as builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -o zip_code main.go

FROM alpine:3.20
COPY --from=builder /app/zip_code .
EXPOSE 8080
ENTRYPOINT ["./zip_code"]
