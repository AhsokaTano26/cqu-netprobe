FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/cqu-netprobe ./cmd/cqu-netprobe

FROM alpine:3.22
RUN addgroup -S netprobe && adduser -S -G netprobe netprobe
COPY --from=build /out/cqu-netprobe /usr/local/bin/cqu-netprobe
USER netprobe
ENTRYPOINT ["/usr/local/bin/cqu-netprobe"]

