# syntax=docker/dockerfile:1

# Build Image
FROM golang:1.25.5-alpine3.23 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /app/fritzbox_exporter .
RUN cp metrics.json metrics-lua.json metrics-api.json /app/

# Runtime Image
FROM alpine:3.23 AS runtime-image

ARG REPO=sberk42/fritzbox_exporter

LABEL org.opencontainers.image.source https://github.com/${REPO}

ENV USERNAME username
ENV PASSWORD password
ENV GATEWAY_URL http://fritz.box:49000
ENV GATEWAY_LUAURL http://fritz.box
ENV LISTEN_ADDRESS 0.0.0.0:9042

RUN mkdir /app \
    && addgroup -S -g 1000 fritzbox \
    && adduser -S -u 1000 -G fritzbox fritzbox \
    && chown -R fritzbox:fritzbox /app

WORKDIR /app

COPY --chown=fritzbox:fritzbox --from=builder /app /app

EXPOSE 9042

ENTRYPOINT [ "/app/fritzbox_exporter" ]
