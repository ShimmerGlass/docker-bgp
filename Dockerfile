FROM --platform=$BUILDPLATFORM golang:alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/docker-bgp .

FROM alpine
COPY --from=build /out/docker-bgp /docker-bgp
ENTRYPOINT ["/docker-bgp"]