ARG PROXY_CACHE_REGISTRY
ARG GO_IMAGE=docker.io/library/golang
ARG GO_TAG=1.21
ARG BUILDDIR=/go/src/github.com/meln5674/nodeport-loadbalancer

FROM ${PROXY_CACHE_REGISTRY}${GO_IMAGE}:${GO_TAG} AS build
ARG BUILDDIR
ARG GO_FLAGS
ARG CGO_ENABLED=0
WORKDIR ${BUILDDIR}
COPY go.mod go.sum ./ 
RUN go mod download
COPY main.go ./
RUN go build -a -tags netgo -ldflags '-w -extldflags "-static"' ${GO_FLAGS} main.go

FROM scratch
ARG BUILDDIR
COPY --from=build ${BUILDDIR}/main /main
ENTRYPOINT ["/main"]
