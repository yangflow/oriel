# syntax=docker/dockerfile:1
# The headless server, `oriel serve`, for Docker and Kubernetes. The program is built for the target platform on the
# build platform (no emulation), so one build covers linux/amd64 and linux/arm64. See docs/product.md, "部署".
ARG GO_VERSION=1.27.3
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/oriel . \
 && mkdir /out/data

# MyGo loads system libraries dynamically, also without a window, so the image needs glibc: no Alpine. This base has
# glibc and the CA certificates for HTTPS feeds, no shell and no GTK, and the unprivileged user 65532.
FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/oriel /oriel
# A named volume on /data takes this owner the first time it is mounted.
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME /data
EXPOSE 7470
# Without ORIEL_PASSWORD the server listens on 127.0.0.1 only and nothing outside the container reaches it.
ENTRYPOINT ["/oriel"]
CMD ["serve", "-data", "/data", "-port", "7470"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD ["/oriel", "healthcheck", "-port", "7470"]
