FROM golang:1.15-alpine3.12 as build
ARG TARGETOS
ARG TARGETARCH

WORKDIR /tmp/kube_plex

RUN apk --no-cache add git alpine-sdk
COPY . .
RUN GO111MODULE=on go mod vendor
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags '-s -w' -o kube_plex ./

FROM scratch
LABEL name="kube-plex"

WORKDIR /root
COPY --from=build /tmp/kube_plex/kube_plex kube_plex
