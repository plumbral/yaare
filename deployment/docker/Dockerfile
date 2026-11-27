FROM --platform=$BUILDPLATFORM oven/bun:1.4.4 AS bun
FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm AS build
COPY --from=bun /usr/local/bin/bun /usr/local/bin/bun
WORKDIR /src
COPY . .
ARG GOPROXY=https://proxy.golang.org,direct
ARG YAARE_RELEASE_VERSION
RUN bun install --frozen-lockfile && bun run build:browser
ARG TARGETARCH
RUN version=${YAARE_RELEASE_VERSION:-$(bun -p 'require("./package.json").version')} && \
    CGO_ENABLED=0 GOOS=linux GOARCH="$TARGETARCH" go build -trimpath -ldflags="-s -w -X yaare/internal/app.version=$version" -o /out/yaare-web ./cmd/yaare-web
RUN chmod -R a+rX /src/build/browser /src/resources/licenses \
    && chmod a+r /src/LICENSE \
    && install -d -m 0700 /out/volumes/data /out/volumes/keys

FROM debian:12-slim
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/doc/ca-certificates/copyright /usr/share/doc/ca-certificates/copyright
COPY --from=build /out/yaare-web /app/yaare-web
COPY --from=build /src/build/browser /app/public
COPY --from=build /src/resources/licenses /app/licenses
COPY --from=build /src/LICENSE /app/LICENSE
COPY --from=build --chown=10001:10001 /out/volumes/ /
USER 10001:10001
ENV HOME=/data YAARE_DATA_DIR=/data YAARE_KEY_FILE=/keys/instance.key
EXPOSE 4180
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/app/yaare-web","--healthcheck"]
ENTRYPOINT ["/app/yaare-web"]
CMD ["--listen=0.0.0.0:4180","--assets=/app/public"]
