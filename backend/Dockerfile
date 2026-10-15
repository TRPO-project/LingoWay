ARG GO_VERSION=1.27

FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd

FROM alpine:3.20
WORKDIR /app
COPY --from=build /app/server .
CMD ["./server"]