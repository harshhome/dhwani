FROM golang:1.23-alpine AS build
WORKDIR /src
COPY src/go.mod .
COPY src/go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src/. .
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/dhwani ./cmd/dhwani

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
RUN addgroup -S dhwani && adduser -S dhwani -G dhwani
WORKDIR /app
COPY --from=build /out/dhwani /usr/local/bin/dhwani
RUN mkdir -p /app/data && chown -R dhwani:dhwani /app
USER dhwani
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/dhwani"]
